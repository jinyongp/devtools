package cli

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/jinyongp/devtools/internal/network"
	"github.com/jinyongp/devtools/internal/proxy"
)

func TestHostPolicyPublicDefaultsAndReplay(t *testing.T) {
	for _, value := range []any{
		map[string]any{"item": network.GlobalRoute{Name: "app"}},
		map[string]any{"item": map[string]any{"name": "app", "reservation": "backend"}},
		[]proxy.Item{{Kind: "route"}},
	} {
		body, err := json.Marshal(publicHostPolicy(value))
		if err != nil || !strings.Contains(string(body), `"host_policy":"rewrite"`) {
			t.Fatalf("effective policy = %s, %v", body, err)
		}
	}
	body, _ := json.Marshal(publicHostPolicy([]proxy.Item{{Kind: "route", HostPolicy: network.HostPreserve}}))
	if !strings.Contains(string(body), `"host_policy":"preserve"`) {
		t.Fatal(string(body))
	}
}
