package proxy

import (
	"net/http/httptest"
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
			request.TLS.ServerName = tc.host
			if tc.password != "" {
				request.SetBasicAuth("reader", tc.password)
			}
			response := httptest.NewRecorder()
			x.serve(response, request)
			if response.Code != tc.status {
				t.Fatal(response.Code, tc.status)
			}
		})
	}
}
