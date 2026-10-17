package proxy

import (
	"context"
	"net"
	"strconv"

	"github.com/jinyongp/devtools/internal/ports"
)

type listenFunc func(network, address string) (net.Listener, error)

func ListenLoopback(port int) ([]net.Listener, error) {
	return listenLoopback(port, func(network, address string) (net.Listener, error) {
		return ports.ListenTCP(context.Background(), network, address)
	})
}

func listenLoopback(port int, listen listenFunc) ([]net.Listener, error) {
	value := strconv.Itoa(port)
	ipv4, err := listen("tcp4", net.JoinHostPort("127.0.0.1", value))
	if err != nil {
		return nil, err
	}
	ipv6, err := listen("tcp6", net.JoinHostPort("::1", value))
	if err == nil {
		return []net.Listener{ipv4, ipv6}, nil
	}
	if ports.BindFamilyUnavailable(err) {
		return []net.Listener{ipv4}, nil
	}
	_ = ipv4.Close()
	return nil, err
}
