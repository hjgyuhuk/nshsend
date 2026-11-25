package shared

import (
	"fmt"
	"net/netip"
	"strconv"
	"strings"
)

// PeerAddr stores an IP address and optional IPv6 zone/scope index.
type PeerAddr struct {
	IP      netip.Addr
	ScopeID uint32
}

// ParsePeerAddr parses a plain IP or scoped IPv6 string (e.g. "fe80::1%3").
func ParsePeerAddr(raw string) (PeerAddr, error) {
	ipPart := raw
	var scope uint32
	if idx := strings.IndexByte(raw, '%'); idx >= 0 {
		ipPart = raw[:idx]
		zoneStr := raw[idx+1:]
		if parsed, err := strconv.ParseUint(zoneStr, 10, 32); err == nil {
			scope = uint32(parsed)
		}
	}
	addr, err := netip.ParseAddr(ipPart)
	if err != nil {
		return PeerAddr{}, err
	}
	return PeerAddr{IP: addr, ScopeID: scope}, nil
}

// FormatDialHost returns the host string (without brackets) suitable for net.JoinHostPort.
func (p PeerAddr) FormatDialHost() string {
	if p.IP.Is6() && p.IP.IsLinkLocalUnicast() && p.ScopeID != 0 {
		return fmt.Sprintf("%s%%%d", p.IP.String(), p.ScopeID)
	}
	return p.IP.String()
}

// FormatURLHost returns the authority host formatted for http.Request URLs (RFC 6874).
func (p PeerAddr) FormatURLHost() string {
	if p.IP.Is4() {
		return p.IP.String()
	}
	if p.IP.IsLinkLocalUnicast() && p.ScopeID != 0 {
		return fmt.Sprintf("[%s%%25%d]", p.IP.String(), p.ScopeID)
	}
	return "[" + p.IP.String() + "]"
}

// AddressPreference ranks address families for dialing:
// 2 = global/ULA IPv6, 1 = IPv4 (or IPv4-mapped IPv6), 0 = link-local IPv6 or other.
func AddressPreference(host string) uint8 {
	ipPart := host
	if idx := strings.IndexByte(host, '%'); idx >= 0 {
		ipPart = host[:idx]
	}
	addr, err := netip.ParseAddr(ipPart)
	if err != nil {
		return 0
	}
	if addr.Is4() || addr.Is4In6() {
		return 1
	}
	if addr.IsGlobalUnicast() && !addr.IsLinkLocalUnicast() {
		return 2
	}
	return 0
}
