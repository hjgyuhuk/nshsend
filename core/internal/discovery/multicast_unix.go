//go:build unix

package discovery

import (
	"fmt"
	"net"
	"syscall"
)

func bindMulticastConnV4(iface net.Interface, ip net.IP, group net.IP, port uint16) (*net.UDPConn, error) {
	fd, err := syscall.Socket(syscall.AF_INET, syscall.SOCK_DGRAM, syscall.IPPROTO_UDP)
	if err != nil {
		return nil, fmt.Errorf("socket v4: %w", err)
	}
	cleanup := true
	defer func() {
		if cleanup {
			_ = syscall.Close(fd)
		}
	}()

	_ = syscall.SetsockoptInt(fd, syscall.SOL_SOCKET, syscall.SO_REUSEADDR, 1)
	_ = syscall.SetsockoptInt(fd, syscall.SOL_SOCKET, 0x0f /* SO_REUSEPORT on Linux/BSD */, 1)
	_ = syscall.SetsockoptInt(fd, syscall.IPPROTO_IP, syscall.IP_MULTICAST_LOOP, 1)

	var sa syscall.SockaddrInet4
	sa.Port = int(port)
	if err := syscall.Bind(fd, &sa); err != nil {
		return nil, fmt.Errorf("bind v4 port %d: %w", port, err)
	}

	g4 := group.To4()
	if4 := ip.To4()
	if g4 == nil || if4 == nil {
		return nil, fmt.Errorf("invalid IPv4 multicast/interface address")
	}

	mreq := &syscall.IPMreq{
		Multiaddr: [4]byte{g4[0], g4[1], g4[2], g4[3]},
		Interface: [4]byte{if4[0], if4[1], if4[2], if4[3]},
	}
	if err := syscall.SetsockoptIPMreq(fd, syscall.IPPROTO_IP, syscall.IP_ADD_MEMBERSHIP, mreq); err != nil {
		return nil, fmt.Errorf("join v4 multicast on %s (%s): %w", iface.Name, if4, err)
	}

	var outIf [4]byte
	copy(outIf[:], if4)
	_ = syscall.SetsockoptInet4Addr(fd, syscall.IPPROTO_IP, syscall.IP_MULTICAST_IF, outIf)

	file := newSocketFile(fd, "multicast-v4")
	defer file.Close()
	packetConn, err := net.FilePacketConn(file)
	if err != nil {
		return nil, fmt.Errorf("file packet conn v4: %w", err)
	}
	cleanup = false
	udpConn, ok := packetConn.(*net.UDPConn)
	if !ok {
		_ = packetConn.Close()
		return nil, fmt.Errorf("unexpected packet conn type %T", packetConn)
	}
	return udpConn, nil
}

func bindMulticastConnV6(iface net.Interface, group net.IP, port uint16) (*net.UDPConn, error) {
	fd, err := syscall.Socket(syscall.AF_INET6, syscall.SOCK_DGRAM, syscall.IPPROTO_UDP)
	if err != nil {
		return nil, fmt.Errorf("socket v6: %w", err)
	}
	cleanup := true
	defer func() {
		if cleanup {
			_ = syscall.Close(fd)
		}
	}()

	_ = syscall.SetsockoptInt(fd, syscall.SOL_SOCKET, syscall.SO_REUSEADDR, 1)
	_ = syscall.SetsockoptInt(fd, syscall.SOL_SOCKET, 0x0f /* SO_REUSEPORT */, 1)
	_ = syscall.SetsockoptInt(fd, syscall.IPPROTO_IPV6, syscall.IPV6_V6ONLY, 1)
	_ = syscall.SetsockoptInt(fd, syscall.IPPROTO_IPV6, syscall.IPV6_MULTICAST_LOOP, 1)

	var sa syscall.SockaddrInet6
	sa.Port = int(port)
	if err := syscall.Bind(fd, &sa); err != nil {
		return nil, fmt.Errorf("bind v6 port %d: %w", port, err)
	}

	g16 := group.To16()
	if g16 == nil {
		return nil, fmt.Errorf("invalid IPv6 multicast group")
	}

	mreq := &syscall.IPv6Mreq{
		Interface: uint32(iface.Index),
	}
	copy(mreq.Multiaddr[:], g16)
	if err := syscall.SetsockoptIPv6Mreq(fd, syscall.IPPROTO_IPV6, syscall.IPV6_JOIN_GROUP, mreq); err != nil {
		return nil, fmt.Errorf("join v6 multicast on %s: %w", iface.Name, err)
	}
	_ = syscall.SetsockoptInt(fd, syscall.IPPROTO_IPV6, syscall.IPV6_MULTICAST_IF, iface.Index)

	file := newSocketFile(fd, "multicast-v6")
	defer file.Close()
	packetConn, err := net.FilePacketConn(file)
	if err != nil {
		return nil, fmt.Errorf("file packet conn v6: %w", err)
	}
	cleanup = false
	udpConn, ok := packetConn.(*net.UDPConn)
	if !ok {
		_ = packetConn.Close()
		return nil, fmt.Errorf("unexpected packet conn type %T", packetConn)
	}
	return udpConn, nil
}
