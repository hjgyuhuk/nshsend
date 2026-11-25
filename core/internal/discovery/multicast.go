package discovery

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"os"
	"sync"
	"time"

	"nshsend/core/internal/shared"
)

var announceDelays = []time.Duration{
	100 * time.Millisecond,
	500 * time.Millisecond,
	2000 * time.Millisecond,
}

func newSocketFile(fd int, name string) *os.File {
	return os.NewFile(uintptr(fd), name)
}

type multicastSocket struct {
	conn   *net.UDPConn
	target *net.UDPAddr
	label  string
}

// MulticastAnnouncement is emitted whenever another LocalSend device announces via UDP.
type MulticastAnnouncement struct {
	Source  shared.PeerAddr
	Message shared.MulticastMessageV2
}

// MulticastService manages per-interface IPv4/IPv6 UDP multicast discovery.
type MulticastService struct {
	mu      sync.RWMutex
	sockets []multicastSocket
	device  shared.MulticastMessageV2
	cancel  context.CancelFunc
	wg      sync.WaitGroup
}

// StartMulticastService binds multicast sockets across usable interfaces and begins listening.
func StartMulticastService(
	groupV4 string,
	groupV6 string,
	port uint16,
	device shared.MulticastMessageV2,
	onAnnounce func(MulticastAnnouncement),
) (*MulticastService, error) {
	g4 := net.ParseIP(groupV4)
	if g4 == nil || g4.To4() == nil {
		return nil, fmt.Errorf("invalid IPv4 multicast group %q", groupV4)
	}
	g6 := net.ParseIP(groupV6)

	ifaces, err := net.Interfaces()
	if err != nil {
		return nil, fmt.Errorf("list network interfaces: %w", err)
	}

	var sockets []multicastSocket
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagMulticast == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, addrErr := iface.Addrs()
		if addrErr != nil {
			continue
		}
		hasV6 := false
		for _, a := range addrs {
			ipNet, ok := a.(*net.IPNet)
			if !ok || ipNet.IP == nil {
				continue
			}
			if ip4 := ipNet.IP.To4(); ip4 != nil {
				if conn, bindErr := bindMulticastConnV4(iface, ip4, g4, port); bindErr == nil {
					sockets = append(sockets, multicastSocket{
						conn:   conn,
						target: &net.UDPAddr{IP: g4, Port: int(port)},
						label:  fmt.Sprintf("%s/%s", iface.Name, ip4.String()),
					})
				}
			} else if g6 != nil && !hasV6 && ipNet.IP.To16() != nil {
				hasV6 = true
				if conn, bindErr := bindMulticastConnV6(iface, g6, port); bindErr == nil {
					sockets = append(sockets, multicastSocket{
						conn:   conn,
						target: &net.UDPAddr{IP: g6, Port: int(port), Zone: iface.Name},
						label:  fmt.Sprintf("%s/v6", iface.Name),
					})
				}
			}
		}
	}

	if len(sockets) == 0 {
		return nil, fmt.Errorf("no network interfaces bound for multicast port %d", port)
	}

	ctx, cancel := context.WithCancel(context.Background())
	svc := &MulticastService{
		sockets: sockets,
		device:  device,
		cancel:  cancel,
	}

	for _, s := range sockets {
		sock := s
		svc.wg.Add(1)
		go func() {
			defer svc.wg.Done()
			svc.receiveLoop(ctx, sock, onAnnounce)
		}()
	}

	return svc, nil
}

// TriggerAnnounceBurst sends repeated multicast announcements so peers discover us reliably.
func (m *MulticastService) TriggerAnnounceBurst(ctx context.Context) {
	payload, err := json.Marshal(shared.AnnouncedMessageV2{
		Alias:       m.device.Alias,
		Version:     m.device.Version,
		DeviceModel: m.device.DeviceModel,
		DeviceType:  m.device.DeviceType,
		Fingerprint: m.device.Fingerprint,
		Port:        m.device.Port,
		Protocol:    m.device.Protocol,
		Download:    m.device.Download,
		Announce:    true,
	})
	if err != nil {
		return
	}

	for _, delay := range announceDelays {
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}

		m.mu.RLock()
		sockets := make([]multicastSocket, len(m.sockets))
		copy(sockets, m.sockets)
		m.mu.RUnlock()

		for _, s := range sockets {
			_, _ = s.conn.WriteToUDP(payload, s.target)
		}
	}
}

// StopMulticastService closes all multicast sockets and waits for receive goroutines.
func (m *MulticastService) StopMulticastService() {
	m.cancel()
	m.mu.Lock()
	for _, s := range m.sockets {
		_ = s.conn.Close()
	}
	m.sockets = nil
	m.mu.Unlock()
	m.wg.Wait()
}

func (m *MulticastService) receiveLoop(ctx context.Context, sock multicastSocket, onAnnounce func(MulticastAnnouncement)) {
	buf := make([]byte, 65536)
	consecutiveErrs := 0
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}
		n, srcAddr, err := sock.conn.ReadFromUDP(buf)
		if err != nil {
			if errors.Is(err, net.ErrClosed) || ctx.Err() != nil {
				return
			}
			consecutiveErrs++
			if consecutiveErrs >= 10 {
				return
			}
			continue
		}
		consecutiveErrs = 0

		var msg shared.MulticastMessageV2
		if err := json.Unmarshal(buf[:n], &msg); err != nil {
			continue
		}
		if msg.Fingerprint == "" || msg.Fingerprint == m.device.Fingerprint {
			continue
		}
		ip, ok := netip.AddrFromSlice(srcAddr.IP)
		if !ok {
			continue
		}
		ip = ip.Unmap()
		var scope uint32
		if srcAddr.Zone != "" {
			if iface, ifaceErr := net.InterfaceByName(srcAddr.Zone); ifaceErr == nil {
				scope = uint32(iface.Index)
			}
		}

		onAnnounce(MulticastAnnouncement{
			Source:  shared.PeerAddr{IP: ip, ScopeID: scope},
			Message: msg,
		})
	}
}
