package diagnostics

import (
	"context"

	"github.com/jinyongp/devtools/internal/ca"
	"github.com/jinyongp/devtools/internal/expose"
	"github.com/jinyongp/devtools/internal/hosts"
	"github.com/jinyongp/devtools/internal/netops"
	"github.com/jinyongp/devtools/internal/netsetup"
	"github.com/jinyongp/devtools/internal/protocol"
	"github.com/jinyongp/devtools/internal/proxy"
	"github.com/jinyongp/devtools/internal/truststore"
)

type Network struct {
	Section
	Listeners         []map[string]any `json:"listeners"`
	Routes            []proxy.Item     `json:"routes"`
	Exposures         []expose.Public  `json:"exposures"`
	CA                ca.Status        `json:"ca"`
	Trusted           bool             `json:"trusted"`
	Setup             map[string]any   `json:"setup"`
	Hosts             map[string]any   `json:"hosts"`
	Isolated          bool             `json:"isolated"`
	Boundaries        []string         `json:"boundaries"`
	Remedies          []map[string]any `json:"remedies"`
	PendingOperations []string         `json:"pending_operations"`
}

func inspectNetwork(ctx context.Context, input Input) (Network, *protocol.Error) {
	out := Network{Section: healthy(), Listeners: []map[string]any{}, Routes: []proxy.Item{}, Exposures: []expose.Public{}, Setup: map[string]any{}, Hosts: map[string]any{}, Isolated: input.IsolatedRoot != "", Boundaries: []string{"Kernel TCP ports and external provider configuration are shared with other applications."}, Remedies: []map[string]any{}}
	m := proxy.Manager{Data: input.Data, IsolatedRoot: input.IsolatedRoot}
	var e *protocol.Error
	out.Listeners, e = m.NetworkStatuses(ctx)
	if e != nil {
		return out, e
	}
	out.Routes, e = proxy.NewResolver(proxy.PortStore(m)).List(ctx, "")
	if e != nil {
		return out, e
	}
	for i := range out.Routes {
		if out.Routes[i].Kind == "route" {
			out.Routes[i].HostPolicy = out.Routes[i].HostPolicy.Effective()
		}
	}
	out.Exposures, e = m.ExposureList(ctx)
	if e != nil {
		return out, e
	}
	out.CA, e = (ca.Store{Data: input.Data}).Status()
	if e != nil {
		return out, e
	}
	if out.CA.Present {
		authority, e := (ca.Store{Data: input.Data}).Read()
		if e != nil {
			return out, e
		}
		out.Trusted = truststore.Trusted(authority)
	}
	out.Setup, e = (netsetup.Manager{Data: input.Data, Isolated: out.Isolated}).Check()
	if e != nil {
		return out, e
	}
	out.Hosts, e = (hosts.Manager{Data: input.Data, Isolated: out.Isolated}).Status()
	if e != nil {
		return out, e
	}
	out.PendingOperations, e = (netops.Store{Data: input.Data}).PendingTLS()
	if e != nil {
		return out, e
	}
	if out.Isolated {
		out.Boundaries = append(out.Boundaries, "Config, data, cache and runtime state are isolated; shared hosts, OS trust and executable capability mutations are disabled.")
	}
	add := func(argv, required []string, message string) {
		out.Remedies = append(out.Remedies, map[string]any{"argv": argv, "required_inputs": required, "message": message})
	}
	if out.CA.Present && !out.Trusted {
		add([]string{"devtools", "proxy", "ca", "export", "--output"}, []string{"public certificate output path"}, "The server can use HTTPS. Clients must trust the public CA or supply it explicitly.")
	}
	if out.Setup["recovery_required"] == true {
		add([]string{"devtools", "proxy", "setup", "--request-id"}, []string{"request UUID"}, "Reapply low-port setup to the installed executable after replacement.")
	}
	if out.Hosts["pending"] == true {
		add([]string{"devtools", "proxy", "hosts", "preview"}, []string{"route selection"}, "Review and resume the pending owned hosts operation from the initiating CLI.")
	}
	for _, item := range out.Exposures {
		if item.State == "auth_required" || item.State == "recovery_required" {
			add([]string{"devtools", "expose", "stop", item.ID, "--request-id"}, []string{"request UUID"}, "Ingress is closed. Reconcile this exposure, then start a new operation with authentication from stdin or a private file.")
		}
	}
	return out, nil
}
