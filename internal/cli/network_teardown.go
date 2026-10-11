package cli

import (
	"context"

	"github.com/jinyongp/devtools/internal/ca"
	"github.com/jinyongp/devtools/internal/project"
	"github.com/jinyongp/devtools/internal/protocol"
	"github.com/jinyongp/devtools/internal/proxy"
	"github.com/jinyongp/devtools/internal/truststore"
)

func (a *App) registerNetworkTeardown() {
	c := Command{Name: "proxy teardown", Description: "Preview or apply selected listener cleanup. Shared CA removal requires explicit --all --remove-ca and confirmed OS untrust.", Options: []Option{{Name: "listener", Pattern: project.ProfilePattern, Description: "Listener name; omission selects default."}, {Name: "all", Boolean: true, Description: "Select all configured listeners and exposures."}, {Name: "remove-ca", Boolean: true, Description: "With --all, include shared CA removal after untrust."}, {Name: "apply", Description: "Digest returned by the teardown preview."}, {Name: "request-id", Pattern: uuidPattern, Description: "Required UUID when applying a preview."}}, Output: map[string]any{"type": "object"}}
	plan := object(map[string]any{"digest": stringSchema(), "selection": object(map[string]any{"listener": stringSchema(), "all": boolSchema(), "remove_ca": boolSchema()}, "listener", "all", "remove_ca"), "listeners": arraySchema(listenerDefinitionSchema()), "routes": arraySchema(routeRefSchema()), "exposures": arraySchema(exposureSchema()), "ca_fingerprint": stringSchema(), "legacy_attempt": stringSchema(), "runtime": map[string]any{"type": "object", "additionalProperties": stringSchema()}}, "digest", "selection", "listeners", "routes", "exposures", "ca_fingerprint", "legacy_attempt", "runtime")
	c.Output = map[string]any{"oneOf": []any{object(map[string]any{"item": plan}, "item"), mutationSchema(map[string]any{"item": plan}, "item")}}
	c.Run = func(ctx context.Context, streams IO, r Request) (any, *protocol.Error) {
		m, e := a.proxyStore()
		if e != nil {
			return nil, e
		}
		hm, e := a.hostsManager(streams)
		if e != nil {
			return nil, e
		}
		selection := proxy.TeardownSelection{Listener: r.Options["listener"], All: r.Options["all"] == "true", RemoveCA: r.Options["remove-ca"] == "true"}
		if r.Options["apply"] == "" {
			if r.Options["request-id"] != "" {
				return nil, argumentError("A request UUID is used only with --apply DIGEST.", "request-id")
			}
			plan, e := m.TeardownPreview(ctx, selection, hm)
			return map[string]any{"item": plan}, e
		}
		if r.Options["request-id"] == "" {
			return nil, argumentError("Applying teardown requires --request-id UUID.", "request-id")
		}
		trust := proxy.TeardownTrust{Untrust: func(ctx context.Context, authority *ca.Authority) *protocol.Error {
			return (truststore.Manager{Terminal: interactive(streams)}).Apply(ctx, "untrust", authority)
		}}
		return m.Teardown(ctx, selection, r.Options["apply"], r.Options["request-id"], hm, trust)
	}
	a.commands = append(a.commands, c)
}
