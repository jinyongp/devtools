package cli

import (
	"github.com/jinyongp/devtools/internal/network"
	"github.com/jinyongp/devtools/internal/proxy"
)

// Public responses show the effective default without changing stored route
// metadata or the resolver's persisted exposure identity projection.
func publicHostPolicy(value any) any {
	switch v := value.(type) {
	case network.GlobalRoute:
		return struct {
			network.GlobalRoute
			HostPolicy network.HostPolicy `json:"host_policy"`
		}{v, v.HostPolicy.Effective()}
	case proxy.Item:
		if v.Kind != "route" {
			return v
		}
		return struct {
			proxy.Item
			HostPolicy network.HostPolicy `json:"host_policy"`
		}{v, v.HostPolicy.Effective()}
	case []proxy.Item:
		out := make([]any, len(v))
		for i, item := range v {
			out[i] = publicHostPolicy(item)
		}
		return out
	case []network.GlobalRoute:
		out := make([]any, len(v))
		for i, item := range v {
			out[i] = publicHostPolicy(item)
		}
		return out
	case []any:
		for i, item := range v {
			v[i] = publicHostPolicy(item)
		}
	case map[string]any:
		// Replayed operation results have already passed through JSON storage.
		if _, ok := v["reservation"]; ok {
			policy, _ := v["host_policy"].(string)
			v["host_policy"] = network.HostPolicy(policy).Effective()
		}
		for _, key := range []string{"item", "items"} {
			if nested, ok := v[key]; ok {
				v[key] = publicHostPolicy(nested)
			}
		}
	}
	return value
}

func hostPolicySchema() map[string]any {
	return map[string]any{"enum": []string{"rewrite", "preserve"}}
}
