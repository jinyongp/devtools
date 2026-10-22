package proxy

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/jinyongp/devtools/internal/network"
	"github.com/jinyongp/devtools/internal/project"
	"github.com/jinyongp/devtools/internal/tasks"
)

func TestHostPolicyGlobalStorageAndRetry(t *testing.T) {
	for _, policy := range []network.HostPolicy{"", network.HostRewrite, network.HostPreserve} {
		t.Run(string(policy.Effective())+string(policy), func(t *testing.T) {
			m := Manager{Data: t.TempDir()}
			ctx, id := context.Background(), tasks.ID()
			result, err := m.AddGlobalWithHostPolicy(ctx, "app", 4400, "app.localhost", "default", "auto", policy, id)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := m.AddGlobalWithHostPolicy(ctx, "app", 4400, "app.localhost", "default", "auto", policy, id); err != nil {
				t.Fatal("identical retry failed", err)
			}
			other := network.HostPreserve
			if policy == network.HostPreserve {
				other = network.HostRewrite
			}
			if _, err := m.AddGlobalWithHostPolicy(ctx, "app", 4400, "app.localhost", "default", "auto", other, id); err == nil || err.Code != "request_conflict" {
				t.Fatal("changed policy replayed", err)
			}
			state, err := m.portStore().Read()
			if err != nil {
				t.Fatal(err)
			}
			route := state.GlobalRoute("app")
			if route.HostPolicy != policy.Canonical() {
				t.Fatal("stored policy", route)
			}
			body, _ := json.Marshal(route)
			if strings.Contains(string(body), "host_policy") != (policy == network.HostPreserve) {
				t.Fatalf("default metadata changed: %s", body)
			}
			if result == nil {
				t.Fatal("missing result")
			}
		})
	}
}

func TestHostPolicyDefaultNetworkDigestProjection(t *testing.T) {
	ref := network.RouteRef{Scope: "global", RouteID: tasks.ID()}
	listener := network.Listener{ID: tasks.ID(), Name: "default", Generation: tasks.ID(), HTTPReservation: "http", HTTPHost: "127.0.0.1"}
	// Protocol 10's persisted projection: field names/order and omitted policy
	// must remain identical for both omitted and explicitly selected rewrite.
	legacy := project.Digest(struct {
		Ref                  network.RouteRef
		Host, DNS            string
		Listener             network.Listener
		HTTP, HTTPS, Backend int
	}{ref, "app.localhost", "auto", listener, 20200, 0, 4400})
	for _, policy := range []network.HostPolicy{"", network.HostRewrite, network.HostPreserve} {
		got := routeNetworkDigest(ref, "app.localhost", "auto", listener, 20200, 0, 4400, policy)
		if (got == legacy) != (policy != network.HostPreserve) {
			t.Fatalf("policy %q changed the wrong identity", policy)
		}
	}
}
