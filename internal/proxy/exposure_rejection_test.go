package proxy

import (
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jinyongp/devtools/internal/expose"
)

func TestExposureRejectsInvalidRequestsBeforeIdentityIO(t *testing.T) {
	for _, tc := range []struct {
		name, host, password string
		status               int
	}{
		{"authority", "wrong.example", "password", 403},
		{"missing auth", "public.example", "", 401},
		{"wrong auth", "public.example", "wrong", 401},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// No owner is available: rejected requests must not touch registry/files.
			x := &exposureRuntime{record: expose.Record{Public: expose.Public{State: "active", Auth: true, URL: "https://public.example"}}, auth: "reader:password"}
			request := httptest.NewRequest("GET", "https://"+tc.host+"/", nil)
			request.Header.Set("Accept", "text/html")
			request.TLS.ServerName = tc.host
			if tc.password != "" {
				request.SetBasicAuth("reader", tc.password)
			}
			response := httptest.NewRecorder()
			x.serve(response, request)
			if response.Code != tc.status {
				t.Fatal(response.Code, tc.status)
			}
			if tc.status == http.StatusUnauthorized && (response.Header().Get("WWW-Authenticate") == "" || !strings.HasPrefix(response.Header().Get("Content-Type"), "text/plain")) {
				t.Fatal("Basic authentication challenge was replaced", response.Header())
			}
		})
	}
}

func TestExposureFailureGuidanceBeforeIdentityIO(t *testing.T) {
	for _, test := range []struct {
		name, state, url, host, serverName, provider string
		status                                       int
		title                                        string
	}{
		{name: "inactive", state: "stopped", url: "https://public.example", host: "public.example", status: 503, title: "Shared route unavailable"},
		{name: "invalid URL", state: "active", url: "https://%", host: "public.example", status: 403, title: "Sharing address mismatch"},
		{name: "wrong authority", state: "active", url: "https://public.example", host: "wrong.example", status: 403, title: "Sharing address mismatch"},
		{name: "wrong SNI", state: "active", url: "https://public.example", host: "public.example", serverName: "wrong.example", status: 403, title: "Sharing address mismatch"},
		{name: "LAN wrong authority", state: "active", url: "https://public.example", host: "wrong.example", provider: "lan", status: 403, title: "Sharing address mismatch"},
	} {
		for _, accept := range []string{"text/html", "application/json"} {
			t.Run(test.name+"/"+accept, func(t *testing.T) {
				// No owner is installed: these failures must return before registry access.
				x := &exposureRuntime{record: expose.Record{Public: expose.Public{State: test.state, URL: test.url, Provider: test.provider}}}
				request := httptest.NewRequest(http.MethodGet, "http://listener/private/path?token=private-token", nil)
				request.Host = test.host
				request.Header.Set("Accept", accept)
				request.SetBasicAuth("reader", "private-auth")
				if test.serverName != "" {
					request.TLS = &tls.ConnectionState{ServerName: test.serverName}
				}
				response := httptest.NewRecorder()
				x.serve(response, request)
				assertProxyGuidance(t, response, accept, test.status, test.title, "devtools expose list")
			})
		}
	}
}
