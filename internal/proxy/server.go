package proxy

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"time"

	"github.com/jinyongp/devtools/internal/protocol"
)

type RouteLister interface {
	List(context.Context, string) ([]Item, *protocol.Error)
}

type Handler struct {
	Routes    RouteLister
	Transport http.RoundTripper
}

func NewHandler(routes RouteLister) *Handler {
	return &Handler{Routes: routes, Transport: loopbackTransport()}
}

func (h *Handler) ServeHTTP(response http.ResponseWriter, request *http.Request) {
	host, ok := requestHost(request.Host)
	if !ok {
		http.Error(response, "route not found", http.StatusNotFound)
		return
	}
	items, err := h.Routes.List(request.Context(), "")
	if err != nil {
		http.Error(response, "route unavailable", http.StatusServiceUnavailable)
		return
	}
	var matches []Item
	for _, item := range items {
		if item.Kind == "route" && item.Host != nil && *item.Host == host {
			matches = append(matches, item)
		}
	}
	if len(matches) == 0 {
		http.Error(response, "route not found", http.StatusNotFound)
		return
	}
	if len(matches) != 1 || matches[0].Status != StatusReady || matches[0].TargetPort == nil {
		http.Error(response, "route unavailable", http.StatusServiceUnavailable)
		return
	}
	h.forward(response, request, *matches[0].TargetPort)
}

func (h *Handler) forward(response http.ResponseWriter, request *http.Request, port int) {
	target := &url.URL{Scheme: "http", Host: net.JoinHostPort("127.0.0.1", fmt.Sprint(port))}
	proxy := &httputil.ReverseProxy{
		Rewrite: func(proxyRequest *httputil.ProxyRequest) {
			proxyRequest.SetURL(target)
			proxyRequest.SetXForwarded()
			proxyRequest.Out.Host = target.Host
			proxyRequest.Out.Header.Set("X-Forwarded-Host", proxyRequest.In.Host)
			proxyRequest.Out.Header.Set("X-Forwarded-Proto", "http")
		},
		Transport:     h.Transport,
		FlushInterval: -1,
		ErrorHandler: func(writer http.ResponseWriter, _ *http.Request, _ error) {
			http.Error(writer, "backend unavailable", http.StatusBadGateway)
		},
	}
	proxy.ServeHTTP(response, request)
}

func requestHost(value string) (string, bool) {
	if value == "" {
		return "", false
	}
	host := value
	if strings.Contains(value, ":") {
		var err error
		host, _, err = net.SplitHostPort(value)
		if err != nil {
			return "", false
		}
	}
	host = strings.ToLower(host)
	return host, projectHost(host)
}

func projectHost(host string) bool {
	return strings.HasSuffix(host, ".localhost")
}

func loopbackTransport() *http.Transport {
	dialer := &net.Dialer{Timeout: 5 * time.Second, KeepAlive: 30 * time.Second}
	return &http.Transport{
		Proxy: nil,
		DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
			host, _, err := net.SplitHostPort(address)
			if err != nil {
				return nil, fmt.Errorf("invalid upstream address")
			}
			ip := net.ParseIP(host)
			if ip == nil || !ip.IsLoopback() {
				return nil, fmt.Errorf("upstream is not loopback")
			}
			return dialer.DialContext(ctx, network, address)
		},
		ForceAttemptHTTP2: false,
	}
}
