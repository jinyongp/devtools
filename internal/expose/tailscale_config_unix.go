//go:build !windows

package expose

import (
	"context"
	"github.com/jinyongp/devtools/internal/protocol"
	"net"
	"net/http"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var tailscaleTCPCredentials = regexp.MustCompile(`^curl -u:([^\s]+) http://localhost:([0-9]+)/localapi/v0/status$`)

func (p Tailscale) platformConfigAPI(ctx context.Context) (tailscaleConfigAPI, *protocol.Error) {
	data, e := p.run(ctx, "debug", "local-creds")
	if e != nil {
		return nil, Failure("tailscale_atomic_config_unavailable")
	}
	credentials := strings.TrimSpace(string(data))
	network, address, token := "", "", ""
	if match := tailscaleTCPCredentials.FindStringSubmatch(credentials); match != nil {
		port, err := strconv.Atoi(match[2])
		if err != nil || port < 1 || port > 65535 {
			return nil, Failure("tailscale_atomic_config_unavailable")
		}
		network, address, token = "tcp", net.JoinHostPort("127.0.0.1", match[2]), match[1]
	} else if strings.HasPrefix(credentials, "curl --unix-socket ") && strings.HasSuffix(credentials, " http://local-tailscaled.sock/localapi/v0/status") {
		network = "unix"
		address = strings.TrimSuffix(strings.TrimPrefix(credentials, "curl --unix-socket "), " http://local-tailscaled.sock/localapi/v0/status")
		if !filepath.IsAbs(address) || strings.ContainsAny(address, "\r\n\x00") {
			return nil, Failure("tailscale_atomic_config_unavailable")
		}
	} else {
		return nil, Failure("tailscale_atomic_config_unavailable")
	}
	transport := &http.Transport{Proxy: nil, DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, network, address)
	}, DisableKeepAlives: true}
	return tailscaleLocalAPI{client: &http.Client{Transport: transport, Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}, token: token}, nil
}
