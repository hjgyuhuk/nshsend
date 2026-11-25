package discovery

import (
	"context"
	"fmt"
	"sync"
	"time"

	"nshsend/core/internal/identity"
	"nshsend/core/internal/shared"
)

// RegisterPeerFn performs an HTTP/HTTPS POST /api/localsend/v2/register to a peer.
type RegisterPeerFn func(
	ctx context.Context,
	protocol shared.ProtocolType,
	addr shared.PeerAddr,
	port uint16,
	expectedFingerprint string,
	selfInfo shared.RegisterRequestV2,
) (shared.RegisterResponseV2, string, error)

// DiscoveryCoordinator orchestrates multicast announcements, HTTP registration, and the peer store.
type DiscoveryCoordinator struct {
	store      *DeviceStore
	cert       *identity.DeviceCertificate
	registerFn RegisterPeerFn
	onUpdated  func([]PeerDeviceView)

	mu        sync.Mutex
	settings  identity.AppSettings
	multicast *MulticastService
	lastErr   string
}

// NewDiscoveryCoordinator creates a coordinator wired to the given store, identity cert, and register callback.
func NewDiscoveryCoordinator(
	store *DeviceStore,
	cert *identity.DeviceCertificate,
	settings identity.AppSettings,
	registerFn RegisterPeerFn,
	onUpdated func([]PeerDeviceView),
) *DiscoveryCoordinator {
	return &DiscoveryCoordinator{
		store:      store,
		cert:       cert,
		settings:   settings,
		registerFn: registerFn,
		onUpdated:  onUpdated,
	}
}

// StartMulticast starts or restarts UDP multicast discovery with updated settings.
func (c *DiscoveryCoordinator) StartMulticast(settings identity.AppSettings) error {
	c.mu.Lock()
	if c.multicast != nil {
		c.multicast.StopMulticastService()
		c.multicast = nil
	}
	c.settings = settings
	c.mu.Unlock()

	proto := shared.ProtocolHTTP
	if settings.HTTPS {
		proto = shared.ProtocolHTTPS
	}

	device := shared.MulticastMessageV2{
		Alias:       settings.Alias,
		Version:     shared.ProtocolVersionV2,
		DeviceModel: settings.DeviceModel,
		DeviceType:  shared.DeviceDesktop,
		Fingerprint: c.cert.Fingerprint,
		Port:        settings.Port,
		Protocol:    proto,
		Download:    false,
	}

	svc, err := StartMulticastService(
		settings.MulticastGroup,
		shared.DefaultMulticastGroupV6,
		settings.MulticastPort,
		device,
		func(ann MulticastAnnouncement) {
			c.handleMulticastAnnouncement(ann)
		},
	)
	c.mu.Lock()
	if err != nil {
		c.lastErr = err.Error()
		c.mu.Unlock()
		return err
	}
	c.lastErr = ""
	c.multicast = svc
	c.mu.Unlock()
	return nil
}

// StopMulticast stops UDP multicast listeners.
func (c *DiscoveryCoordinator) StopMulticast() {
	c.mu.Lock()
	svc := c.multicast
	c.multicast = nil
	c.mu.Unlock()
	if svc != nil {
		svc.StopMulticastService()
	}
}

// ScanPeers triggers an immediate multicast announcement burst.
func (c *DiscoveryCoordinator) ScanPeers(ctx context.Context) error {
	c.mu.Lock()
	svc := c.multicast
	lastErr := c.lastErr
	c.mu.Unlock()
	if svc == nil {
		if lastErr != "" {
			return fmt.Errorf("multicast unavailable: %s", lastErr)
		}
		return fmt.Errorf("multicast service not running")
	}
	go svc.TriggerAnnounceBurst(ctx)
	return nil
}

// RegisterManualPeer directly registers with a peer by IP/port/protocol without multicast.
func (c *DiscoveryCoordinator) RegisterManualPeer(
	ctx context.Context,
	host string,
	port uint16,
	protocol shared.ProtocolType,
) (PeerDeviceView, error) {
	addr, err := shared.ParsePeerAddr(host)
	if err != nil {
		return PeerDeviceView{}, fmt.Errorf("invalid peer IP %q: %w", host, err)
	}
	if port == 0 {
		port = shared.DefaultPort
	}
	if protocol == "" {
		protocol = shared.ProtocolHTTPS
	}

	selfReq := c.buildSelfRegisterRequest()
	resp, certFP, err := c.registerFn(ctx, protocol, addr, port, "", selfReq)
	if err != nil {
		return PeerDeviceView{}, err
	}

	fp := resp.Fingerprint
	if protocol == shared.ProtocolHTTPS && certFP != "" {
		fp = certFP
	}

	obs := DeviceObservation{
		Alias:       resp.Alias,
		Version:     resp.Version,
		DeviceModel: resp.DeviceModel,
		DeviceType:  resp.DeviceType,
		Fingerprint: fp,
		Channel: HttpChannel{
			Host:     addr.FormatDialHost(),
			Port:     port,
			Protocol: protocol,
		},
		Download: resp.Download,
	}
	c.store.RecordOutboundSuccess(obs, time.Now())
	c.emitDevicesUpdated()

	view, _, ok := c.store.LookupDevice(fp)
	if !ok {
		return PeerDeviceView{}, fmt.Errorf("registered peer missing from store")
	}
	return view, nil
}

// NotifyInboundRegistration records a peer that registered with our HTTP/HTTPS server.
func (c *DiscoveryCoordinator) NotifyInboundRegistration(addr shared.PeerAddr, req shared.RegisterRequestV2) {
	obs := DeviceObservation{
		Alias:       req.Alias,
		Version:     req.Version,
		DeviceModel: req.DeviceModel,
		DeviceType:  req.DeviceType,
		Fingerprint: req.Fingerprint,
		Channel: HttpChannel{
			Host:     addr.FormatDialHost(),
			Port:     req.Port,
			Protocol: req.Protocol,
		},
		Download: req.Download,
	}
	c.store.RecordInboundRegister(obs, time.Now())
	c.emitDevicesUpdated()
}

func (c *DiscoveryCoordinator) handleMulticastAnnouncement(ann MulticastAnnouncement) {
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()

	selfReq := c.buildSelfRegisterRequest()
	expectedFP := ""
	if ann.Message.Protocol == shared.ProtocolHTTPS {
		expectedFP = ann.Message.Fingerprint
	}
	resp, certFP, err := c.registerFn(ctx, ann.Message.Protocol, ann.Source, ann.Message.Port, expectedFP, selfReq)
	ch := HttpChannel{
		Host:     ann.Source.FormatDialHost(),
		Port:     ann.Message.Port,
		Protocol: ann.Message.Protocol,
	}
	if err != nil {
		c.store.RecordOutboundFailure(ann.Message.Fingerprint, ch, time.Now())
		return
	}

	fp := resp.Fingerprint
	if ann.Message.Protocol == shared.ProtocolHTTPS && certFP != "" {
		fp = certFP
	}
	if fp == "" {
		fp = ann.Message.Fingerprint
	}

	c.store.RecordOutboundSuccess(DeviceObservation{
		Alias:       resp.Alias,
		Version:     resp.Version,
		DeviceModel: resp.DeviceModel,
		DeviceType:  resp.DeviceType,
		Fingerprint: fp,
		Channel:     ch,
		Download:    resp.Download,
	}, time.Now())
	c.emitDevicesUpdated()
}

func (c *DiscoveryCoordinator) buildSelfRegisterRequest() shared.RegisterRequestV2 {
	c.mu.Lock()
	s := c.settings
	c.mu.Unlock()

	proto := shared.ProtocolHTTP
	if s.HTTPS {
		proto = shared.ProtocolHTTPS
	}
	return shared.RegisterRequestV2{
		Alias:       s.Alias,
		Version:     shared.ProtocolVersionV2,
		DeviceModel: s.DeviceModel,
		DeviceType:  shared.DeviceDesktop,
		Fingerprint: c.cert.Fingerprint,
		Port:        s.Port,
		Protocol:    proto,
		Download:    false,
	}
}

func (c *DiscoveryCoordinator) emitDevicesUpdated() {
	if c.onUpdated != nil {
		c.onUpdated(c.store.ConfirmedDevices())
	}
}
