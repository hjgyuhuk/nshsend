package discovery

import (
	"testing"
	"time"

	"nshsend/core/internal/shared"
)

func TestDeviceStoreDedupeAndHTTPSDowngradeGuard(t *testing.T) {
	store := NewDeviceStore()
	now := time.Now()

	store.RecordOutboundSuccess(DeviceObservation{
		Alias:       "Phone",
		Version:     "2.2",
		DeviceType:  shared.DeviceMobile,
		Fingerprint: "FP-1",
		Channel:     HttpChannel{Host: "192.168.1.20", Port: 53317, Protocol: shared.ProtocolHTTPS},
	}, now)

	// Plain HTTP observation claiming same fingerprint must be ignored after HTTPS verification.
	store.RecordOutboundSuccess(DeviceObservation{
		Alias:       "Spoofed",
		Version:     "2.2",
		DeviceType:  shared.DeviceDesktop,
		Fingerprint: "FP-1",
		Channel:     HttpChannel{Host: "10.0.0.99", Port: 53317, Protocol: shared.ProtocolHTTP},
	}, now.Add(time.Second))

	devices := store.ConfirmedDevices()
	if len(devices) != 1 {
		t.Fatalf("expected 1 device, got %d", len(devices))
	}
	if devices[0].Alias != "Phone" || devices[0].Protocol != shared.ProtocolHTTPS || !devices[0].Verified {
		t.Fatalf("unexpected device state after downgrade attempt: %+v", devices[0])
	}
}

func TestDeviceStoreChannelRanking(t *testing.T) {
	store := NewDeviceStore()
	now := time.Now()

	store.RecordInboundRegister(DeviceObservation{
		Alias:       "Peer",
		Version:     "2.2",
		Fingerprint: "FP-2",
		Channel:     HttpChannel{Host: "fe80::1%2", Port: 53317, Protocol: shared.ProtocolHTTPS},
	}, now)
	store.RecordOutboundSuccess(DeviceObservation{
		Alias:       "Peer",
		Version:     "2.2",
		Fingerprint: "FP-2",
		Channel:     HttpChannel{Host: "192.168.1.15", Port: 53317, Protocol: shared.ProtocolHTTPS},
	}, now.Add(time.Second))

	_, ranked, ok := store.LookupDevice("FP-2")
	if !ok || len(ranked) != 2 {
		t.Fatalf("expected 2 ranked channels, got %d (ok=%v)", len(ranked), ok)
	}
	if ranked[0].Host != "192.168.1.15" {
		t.Fatalf("expected reachable IPv4 channel first, got %s", ranked[0].Host)
	}
}
