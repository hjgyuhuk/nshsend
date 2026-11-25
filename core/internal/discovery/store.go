package discovery

import (
	"net"
	"sort"
	"sync"
	"time"

	"nshsend/core/internal/shared"
)

// OutboundStatus tracks reachability from this device to a peer endpoint.
type OutboundStatus uint8

const (
	OutboundUntested OutboundStatus = iota
	OutboundReachable
	OutboundFailed
)

func (s OutboundStatus) rank() uint8 {
	switch s {
	case OutboundReachable:
		return 2
	case OutboundUntested:
		return 1
	default:
		return 0
	}
}

// HttpChannel identifies one reachable HTTP/HTTPS endpoint of a peer.
type HttpChannel struct {
	Host     string              `json:"host"`
	Port     uint16              `json:"port"`
	Protocol shared.ProtocolType `json:"protocol"`
}

func (c HttpChannel) authenticates() bool {
	return c.Protocol == shared.ProtocolHTTPS
}

// ChannelState stores per-direction reachability for one HttpChannel.
type ChannelState struct {
	Outbound   OutboundStatus
	LastChange time.Time
	Inbound    *time.Time
}

// PeerDeviceView is the read-only projection exposed to UI/IPC and senders.
type PeerDeviceView struct {
	Fingerprint string              `json:"fingerprint"`
	Alias       string              `json:"alias"`
	Version     string              `json:"version"`
	DeviceModel string              `json:"deviceModel,omitempty"`
	DeviceType  shared.DeviceType   `json:"deviceType,omitempty"`
	Host        string              `json:"host"`
	Port        uint16              `json:"port"`
	Protocol    shared.ProtocolType `json:"protocol"`
	Verified    bool                `json:"verified"`
	Download    bool                `json:"download"`
	LastSeenMs  int64               `json:"lastSeenMs"`
}

// DeviceObservation describes an HTTP or multicast observation of a peer.
type DeviceObservation struct {
	Alias       string
	Version     string
	DeviceModel string
	DeviceType  shared.DeviceType
	Fingerprint string
	Channel     HttpChannel
	Download    bool
}

type storedDevice struct {
	latest    DeviceObservation
	confirmed bool
	verified  bool
	channels  map[HttpChannel]ChannelState
	lastSeen  time.Time
}

// DeviceStore deduplicates peers by fingerprint and guards verified HTTPS peers
// against unauthenticated HTTP downgrade/spoofing.
type DeviceStore struct {
	mu      sync.RWMutex
	devices map[string]*storedDevice
	order   []string
}

// NewDeviceStore creates an empty in-memory peer store.
func NewDeviceStore() *DeviceStore {
	return &DeviceStore{
		devices: make(map[string]*storedDevice),
	}
}

// RecordOutboundSuccess marks a peer channel as confirmed and reachable.
func (s *DeviceStore) RecordOutboundSuccess(obs DeviceObservation, now time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()

	entry := s.ensureDeviceLocked(obs, now)
	if entry.verified && !obs.Channel.authenticates() {
		return
	}

	s.updateChannelLocked(entry, obs.Channel, OutboundReachable, false, now)
	if obs.Channel.authenticates() {
		entry.verified = true
		for ch := range entry.channels {
			if !ch.authenticates() {
				delete(entry.channels, ch)
			}
		}
	}
	entry.confirmed = true
	entry.latest = obs
	entry.lastSeen = now
}

// RecordInboundRegister marks a peer channel as confirmed via an incoming HTTP/HTTPS request.
func (s *DeviceStore) RecordInboundRegister(obs DeviceObservation, now time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()

	entry := s.ensureDeviceLocked(obs, now)
	if entry.verified && !obs.Channel.authenticates() {
		return
	}

	s.updateChannelLocked(entry, obs.Channel, OutboundUntested, true, now)
	if obs.Channel.authenticates() {
		entry.verified = true
		for ch := range entry.channels {
			if !ch.authenticates() {
				delete(entry.channels, ch)
			}
		}
	}
	entry.confirmed = true
	entry.latest = obs
	entry.lastSeen = now
}

// RecordOutboundFailure records a failed dial attempt on a channel.
func (s *DeviceStore) RecordOutboundFailure(fingerprint string, ch HttpChannel, now time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()

	entry, ok := s.devices[fingerprint]
	if !ok || (entry.verified && !ch.authenticates()) {
		return
	}
	s.updateChannelLocked(entry, ch, OutboundFailed, false, now)
}

// ConfirmedDevices returns all confirmed peers in first-seen order.
func (s *DeviceStore) ConfirmedDevices() []PeerDeviceView {
	s.mu.RLock()
	defer s.mu.RUnlock()

	out := make([]PeerDeviceView, 0, len(s.order))
	for _, fp := range s.order {
		entry := s.devices[fp]
		if entry == nil || !entry.confirmed {
			continue
		}
		best := rankChannelsLocked(entry)
		ch := entry.latest.Channel
		if len(best) > 0 {
			ch = best[0]
		}
		out = append(out, PeerDeviceView{
			Fingerprint: entry.latest.Fingerprint,
			Alias:       entry.latest.Alias,
			Version:     entry.latest.Version,
			DeviceModel: entry.latest.DeviceModel,
			DeviceType:  entry.latest.DeviceType,
			Host:        ch.Host,
			Port:        ch.Port,
			Protocol:    ch.Protocol,
			Verified:    entry.verified,
			Download:    entry.latest.Download,
			LastSeenMs:  entry.lastSeen.UnixMilli(),
		})
	}
	return out
}

// LookupDevice returns a single confirmed device and its ranked channels.
func (s *DeviceStore) LookupDevice(fingerprint string) (PeerDeviceView, []HttpChannel, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	entry, ok := s.devices[fingerprint]
	if !ok || !entry.confirmed {
		return PeerDeviceView{}, nil, false
	}
	ranked := rankChannelsLocked(entry)
	ch := entry.latest.Channel
	if len(ranked) > 0 {
		ch = ranked[0]
	}
	view := PeerDeviceView{
		Fingerprint: entry.latest.Fingerprint,
		Alias:       entry.latest.Alias,
		Version:     entry.latest.Version,
		DeviceModel: entry.latest.DeviceModel,
		DeviceType:  entry.latest.DeviceType,
		Host:        ch.Host,
		Port:        ch.Port,
		Protocol:    ch.Protocol,
		Verified:    entry.verified,
		Download:    entry.latest.Download,
		LastSeenMs:  entry.lastSeen.UnixMilli(),
	}
	return view, ranked, true
}

func (s *DeviceStore) ensureDeviceLocked(obs DeviceObservation, now time.Time) *storedDevice {
	if existing, ok := s.devices[obs.Fingerprint]; ok {
		return existing
	}
	created := &storedDevice{
		latest:   obs,
		channels: make(map[HttpChannel]ChannelState),
		lastSeen: now,
	}
	s.devices[obs.Fingerprint] = created
	s.order = append(s.order, obs.Fingerprint)
	return created
}

func (s *DeviceStore) updateChannelLocked(entry *storedDevice, ch HttpChannel, outbound OutboundStatus, isInbound bool, now time.Time) {
	var existingInbound *time.Time
	existingOutbound := OutboundUntested
	for known, st := range entry.channels {
		if known.Host == ch.Host {
			if !isInbound {
				existingInbound = st.Inbound
				delete(entry.channels, known)
			} else if known == ch {
				existingOutbound = st.Outbound
			} else {
				delete(entry.channels, known)
			}
		}
	}

	if isInbound {
		ts := now
		entry.channels[ch] = ChannelState{
			Outbound:   existingOutbound,
			LastChange: now,
			Inbound:    &ts,
		}
		return
	}

	if outbound == OutboundFailed {
		st := entry.channels[ch]
		st.Outbound = OutboundFailed
		st.LastChange = now
		entry.channels[ch] = st
		return
	}

	entry.channels[ch] = ChannelState{
		Outbound:   outbound,
		LastChange: now,
		Inbound:    existingInbound,
	}
}

func rankChannelsLocked(entry *storedDevice) []HttpChannel {
	type scored struct {
		ch    HttpChannel
		state ChannelState
	}
	items := make([]scored, 0, len(entry.channels))
	for ch, st := range entry.channels {
		items = append(items, scored{ch: ch, state: st})
	}
	sort.Slice(items, func(i, j int) bool {
		ri, rj := items[i].state.Outbound.rank(), items[j].state.Outbound.rank()
		if ri != rj {
			return ri > rj
		}
		pi, pj := shared.AddressPreference(items[i].ch.Host), shared.AddressPreference(items[j].ch.Host)
		if pi != pj {
			return pi > pj
		}
		return items[i].state.LastChange.After(items[j].state.LastChange)
	})
	out := make([]HttpChannel, len(items))
	for i, it := range items {
		out[i] = it.ch
	}
	return out
}

// ListLocalNonLoopbackIPs returns sorted non-loopback IPv4/IPv6 addresses (excluding link-local IPv6).
func ListLocalNonLoopbackIPs() []string {
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil
	}
	seen := make(map[string]struct{})
	var addrs []string
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		ifAddrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, a := range ifAddrs {
			ipNet, ok := a.(*net.IPNet)
			if !ok || ipNet.IP == nil || ipNet.IP.IsLoopback() {
				continue
			}
			if ipNet.IP.To4() == nil && ipNet.IP.IsLinkLocalUnicast() {
				continue
			}
			s := ipNet.IP.String()
			if _, exists := seen[s]; !exists {
				seen[s] = struct{}{}
				addrs = append(addrs, s)
			}
		}
	}
	sort.Strings(addrs)
	return addrs
}
