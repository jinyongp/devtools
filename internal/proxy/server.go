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

	"github.com/jinyongp/devtools/internal/network"
	"github.com/jinyongp/devtools/internal/protocol"
)

type RouteLister interface {
	List(context.Context, string) ([]Item, *protocol.Error)
}

type Handler struct {
	Routes    RouteLister
	Transport http.RoundTripper
	Listener  string
	HTTPSPort int
	Guard     func(*http.Request, Item) bool
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
	if request.TLS != nil && !strings.EqualFold(request.TLS.ServerName, host) {
		http.Error(response, "route not found", http.StatusNotFound)
		return
	}
	decision, err := selectRoute(request.Context(), h.Routes, h.Listener, host)
	if err != nil {
		http.Error(response, "route unavailable", http.StatusServiceUnavailable)
		return
	}
	if decision.matches == 0 {
		http.Error(response, "route not found", http.StatusNotFound)
		return
	}
	if decision.matches != 1 || decision.item.Status != StatusReady || decision.item.TargetPort == nil {
		http.Error(response, "route unavailable", http.StatusServiceUnavailable)
		return
	}
	if h.Guard != nil && !h.Guard(request, decision.item) {
		http.Error(response, "route is not shared", http.StatusForbidden)
		return
	}
	if h.HTTPSPort > 0 && request.TLS == nil {
		authority := host
		if h.HTTPSPort != 443 {
			authority = net.JoinHostPort(host, fmt.Sprint(h.HTTPSPort))
		}
		redirect := *request.URL
		redirect.Scheme = "https"
		redirect.Host = authority
		http.Redirect(response, request, redirect.String(), http.StatusTemporaryRedirect)
		return
	}
	h.forward(response, request, *decision.item.TargetPort)
}

func (h *Handler) forward(response http.ResponseWriter, request *http.Request, port int) {
	target := &url.URL{Scheme: "http", Host: net.JoinHostPort("127.0.0.1", fmt.Sprint(port))}
	proxy := &httputil.ReverseProxy{
		Rewrite: func(proxyRequest *httputil.ProxyRequest) {
			proxyRequest.SetURL(target)
			proxyRequest.SetXForwarded()
			proxyRequest.Out.Host = target.Host
			proxyRequest.Out.Header.Set("X-Forwarded-Host", proxyRequest.In.Host)
			scheme := "http"
			if proxyRequest.In.TLS != nil {
				scheme = "https"
			}
			proxyRequest.Out.Header.Set("X-Forwarded-Proto", scheme)
			if ingress, ok := proxyRequest.In.Context().Value(ingressKey{}).(publicIngress); ok {
				proxyRequest.Out.Header.Set("X-Forwarded-Host", ingress.Authority)
				proxyRequest.Out.Header.Set("X-Forwarded-Proto", ingress.Scheme)
				if ingress.Authenticated {
					proxyRequest.Out.Header.Del("Authorization")
				}
			}
			proxyRequest.Out.Header.Del("Forwarded")
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
	return network.ValidHost(host)
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
		ForceAttemptHTTP2:   false,
		MaxIdleConns:        100,
		MaxIdleConnsPerHost: 2,
		IdleConnTimeout:     90 * time.Second,
	}
}
