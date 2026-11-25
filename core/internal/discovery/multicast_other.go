//go:build !unix

package discovery

import (
	"fmt"
	"net"
)

func bindMulticastConnV4(_ net.Interface, _ net.IP, group net.IP, port uint16) (*net.UDPConn, error) {
	addr := &net.UDPAddr{IP: group, Port: int(port)}
	conn, err := net.ListenMulticastUDP("udp4", nil, addr)
	if err != nil {
		return nil, fmt.Errorf("listen multicast v4: %w", err)
	}
	return conn, nil
}

func bindMulticastConnV6(_ net.Interface, group net.IP, port uint16) (*net.UDPConn, error) {
	addr := &net.UDPAddr{IP: group, Port: int(port)}
	conn, err := net.ListenMulticastUDP("udp6", nil, addr)
	if err != nil {
		return nil, fmt.Errorf("listen multicast v6: %w", err)
	}
	return conn, nil
}
