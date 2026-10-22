package proxy

import (
	"context"
	"crypto/tls"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jinyongp/devtools/internal/protocol"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

func TestHandlerBackendUnavailableGuidance(t *testing.T) {
	for _, test := range []struct {
		name, accept string
		html         bool
	}{
		{"browser", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8", true},
		{"html with parameters", "text/html; charset=utf-8; q=0.8", true},
		{"no accept", "", false},
		{"wildcard", "*/*", false},
		{"json client", "application/json", false},
		{"html excluded", "text/html;q=0.0, text/plain", false},
		{"invalid quality", "text/html;q=invalid", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			handler := NewHandler(routeListFunc(func(context.Context, string) ([]Item, *protocol.Error) {
				return []Item{readyRoute("app.localhost", 3000)}, nil
			}))
			handler.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) {
				return nil, errors.New("dial tcp: private transport error at /private/project")
			})
			request := httptest.NewRequest(http.MethodGet, "http://app.localhost:20200/private/path?token=private-token", nil)
			request.Header.Set("Accept", test.accept)
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != http.StatusBadGateway {
				t.Fatalf("status = %d", response.Code)
			}
			contentType := "text/plain; charset=utf-8"
			if test.html {
				contentType = "text/html; charset=utf-8"
			}
			if response.Header().Get("Content-Type") != contentType {
				t.Fatalf("content type = %q, want %q", response.Header().Get("Content-Type"), contentType)
			}
			if response.Header().Get("Cache-Control") != "no-store" || response.Header().Get("Vary") != "Accept" {
				t.Fatalf("cache headers = %v", response.Header())
			}
			body := response.Body.String()
			for _, want := range []string{"Backend unavailable", "app.localhost:20200", "127.0.0.1:3000", "devtools command list", "devtools run --proxy"} {
				if !strings.Contains(body, want) {
					t.Errorf("missing %q in response: %s", want, body)
				}
			}
			for _, private := range []string{"dial tcp", "private transport error", "/private/", "private-token"} {
				if strings.Contains(body, private) {
					t.Errorf("private detail %q in response", private)
				}
			}
			if test.html && !strings.Contains(body, `href=""`) {
				t.Error("missing reload link")
			}
			if !test.html && strings.Contains(body, "<!doctype html>") {
				t.Error("HTML returned to plain text client")
			}

			// A new request must reach the server once it starts, including its own errors.
			for _, status := range []int{http.StatusNotFound, http.StatusForbidden, http.StatusServiceUnavailable, http.StatusBadGateway} {
				handler.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) {
					return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(`{"error":"application error"}`))}, nil
				})
				response = httptest.NewRecorder()
				handler.ServeHTTP(response, request)
				if response.Code != status || response.Header().Get("Content-Type") != "application/json" || response.Body.String() != `{"error":"application error"}` {
					t.Fatalf("application response was replaced: %d %v %s", response.Code, response.Header(), response.Body.String())
				}
			}
		})
	}
}

func TestHandlerRouteFailureGuidance(t *testing.T) {
	unresolved := readyRoute("app.localhost", 3000)
	unresolved.Status = StatusAssignmentMissing
	unresolved.TargetPort = nil
	unresolved.Directory = "/private/project"
	unresolved.Profile = "private-profile"
	for _, test := range []struct {
		name, host, serverName string
		items                  []Item
		listErr                *protocol.Error
		forbidden              bool
		status                 int
		title, command         string
	}{
		{name: "unknown host", host: "app.localhost", status: 404, title: "Route not found", command: "devtools proxy list"},
		{name: "missing host", status: 404, title: "Route not found", command: "devtools proxy list"},
		{name: "invalid host", host: "<script>alert(1)</script>.localhost", status: 404, title: "Route not found", command: "devtools proxy list"},
		{name: "TLS mismatch", host: "app.localhost", serverName: "other.localhost", items: []Item{readyRoute("app.localhost", 3000)}, status: 404, title: "Route not found", command: "devtools proxy list"},
		{name: "lookup failed", host: "app.localhost", listErr: protocol.NewError("io_error", "private registry error at /private/project", 1, nil), status: 503, title: "Route unavailable", command: "devtools proxy status --all"},
		{name: "duplicate routes", host: "app.localhost", items: []Item{readyRoute("app.localhost", 3000), readyRoute("app.localhost", 3001)}, status: 503, title: "Route unavailable", command: "devtools proxy list"},
		{name: "assignment missing", host: "app.localhost", items: []Item{unresolved}, status: 503, title: "Route unavailable", command: "devtools proxy list"},
		{name: "local only", host: "app.localhost", items: []Item{readyRoute("app.localhost", 3000)}, forbidden: true, status: 403, title: "Route is not shared", command: "devtools expose list"},
	} {
		for _, accept := range []string{"text/html", "application/json"} {
			t.Run(test.name+"/"+accept, func(t *testing.T) {
				handler := NewHandler(routeListFunc(func(context.Context, string) ([]Item, *protocol.Error) {
					return test.items, test.listErr
				}))
				handler.Guard = func(*http.Request, Item) bool { return !test.forbidden }
				handler.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) {
					t.Fatal("rejected request reached backend")
					return nil, nil
				})
				request := httptest.NewRequest(http.MethodGet, "http://listener/private/path?token=private-token", nil)
				request.Host = test.host
				request.Header.Set("Accept", accept)
				if test.serverName != "" {
					request.TLS = &tls.ConnectionState{ServerName: test.serverName}
				}
				response := httptest.NewRecorder()
				handler.ServeHTTP(response, request)
				assertProxyGuidance(t, response, accept, test.status, test.title, test.command)
				if test.host == "" && !strings.Contains(response.Body.String(), "(not provided)") {
					t.Error("missing host was not explained")
				}
			})
		}
	}
}

func assertProxyGuidance(t *testing.T, response *httptest.ResponseRecorder, accept string, status int, title, command string) {
	t.Helper()
	if response.Code != status {
		t.Fatalf("status = %d, want %d", response.Code, status)
	}
	contentType := "text/plain; charset=utf-8"
	if accept == "text/html" {
		contentType = "text/html; charset=utf-8"
	}
	if response.Header().Get("Content-Type") != contentType || response.Header().Get("Cache-Control") != "no-store" || response.Header().Get("Vary") != "Accept" || response.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("unexpected error headers: %v", response.Header())
	}
	body := response.Body.String()
	for _, want := range []string{title, command} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %q in response: %s", want, body)
		}
	}
	for _, private := range []string{"/private/", "private-token", "private-profile", "private registry error", "private-auth", "127.0.0.1:3000"} {
		if strings.Contains(body, private) {
			t.Errorf("private detail %q in response", private)
		}
	}
	if accept == "text/html" {
		if strings.Contains(body, "<script>") || !strings.Contains(body, `href=""`) {
			t.Error("unsafe host markup or missing reload link")
		}
	} else if strings.Contains(body, "<!doctype html>") {
		t.Error("HTML returned to plain text client")
	}
}
