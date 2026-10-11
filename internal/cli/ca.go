package cli

import (
	"context"
	"os"
	"path/filepath"

	"github.com/jinyongp/devtools/internal/ca"
	"github.com/jinyongp/devtools/internal/netops"
	"github.com/jinyongp/devtools/internal/protocol"
	"github.com/jinyongp/devtools/internal/truststore"
	"golang.org/x/term"
)

func (a *App) caStore() (ca.Store, *protocol.Error) {
	d, e := a.dataDirectory()
	return ca.Store{Data: filepath.Dir(d)}, e
}

func interactive(streams IO) bool {
	f, ok := streams.In.(*os.File)
	return ok && term.IsTerminal(int(f.Fd()))
}

func (a *App) registerCA() {
	for _, action := range []string{"init", "status", "export", "trust", "untrust"} {
		name := "proxy ca " + action
		if action == "trust" || action == "untrust" {
			name = "proxy " + action
		}
		c := Command{Name: name, Description: "Manage the local CA and explicit OS trust; exports contain public certificates only.", Output: map[string]any{"type": "object"}}
		switch action {
		case "status":
			c.Output = object(map[string]any{"item": caStatusSchema(), "trusted": boolSchema()}, "item", "trusted")
		case "export":
			c.Output = itemOutput(object(map[string]any{"fingerprint": stringSchema(), "path": stringSchema(), "certificate": stringSchema()}, "fingerprint"))
		case "init":
			c.Output = mutationSchema(map[string]any{"item": object(map[string]any{"fingerprint": stringSchema(), "not_after": stringSchema()}, "fingerprint", "not_after")}, "item")
		default:
			c.Output = mutationSchema(map[string]any{"item": object(map[string]any{"fingerprint": stringSchema(), "trust_action": stringSchema(), "client_restart_may_be_required": boolSchema()}, "fingerprint", "trust_action", "client_restart_may_be_required")}, "item")
		}
		if action == "init" || action == "trust" || action == "untrust" {
			c.Options = []Option{{Name: "request-id", Required: true, Pattern: uuidPattern, Description: "UUID for retry-safe mutation."}}
		}
		if action == "export" {
			c.Options = []Option{{Name: "output", MinLength: 1, Description: "New public CA certificate file; omission returns PEM in JSON."}}
		}
		c.Run = func(ctx context.Context, streams IO, r Request) (any, *protocol.Error) {
			store, e := a.caStore()
			if e != nil {
				return nil, e
			}
			if action == "status" {
				status, e := store.Status()
				if e != nil {
					return nil, e
				}
				trusted := false
				if status.Present {
					authority, e := store.Read()
					if e != nil {
						return nil, e
					}
					trusted = truststore.Trusted(authority)
				}
				return map[string]any{"item": status, "trusted": trusted}, nil
			}
			if action == "export" {
				authority, e := store.Read()
				if e != nil {
					return nil, e
				}
				result := map[string]any{"fingerprint": authority.Fingerprint()}
				if path := r.Options["output"]; path != "" {
					if e := ca.Export(authority, path); e != nil {
						return nil, e
					}
					result["path"] = path
				} else {
					result["certificate"] = string(authority.PEM())
				}
				return map[string]any{"item": result}, nil
			}
			dirs, pathErr := a.currentPaths()
			if pathErr != nil {
				return nil, netops.Failure("io_error", "Cannot resolve user paths.")
			}
			if (action == "trust" || action == "untrust") && dirs.IsolatedRoot != "" {
				return nil, netops.Failure("isolated_operation_not_allowed", "OS trust changes are unavailable in isolated mode; export a CA for the client.")
			}
			input := map[string]any{"action": "ca." + action}
			return (netops.Store{Data: store.Data}).Apply(ctx, r.Options["request-id"], input, []string{"ca"}, func(j *netops.Journal) (any, *protocol.Error) {
				if action == "init" {
					authority, changed, e := store.Init(ctx)
					if e != nil {
						return nil, e
					}
					return map[string]any{"item": map[string]any{"fingerprint": authority.Fingerprint(), "not_after": authority.Certificate.NotAfter}, "changed": changed}, nil
				}
				authority, e := store.Read()
				if e != nil {
					return nil, e
				}
				var expected string
				present, e := j.Get("fingerprint", &expected)
				if e != nil {
					return nil, e
				}
				if present {
					if expected != authority.Fingerprint() {
						return nil, netops.Failure("request_conflict", "CA changed since this operation began.")
					}
				} else if e := j.Set("fingerprint", authority.Fingerprint()); e != nil {
					return nil, e
				}
				manager := truststore.Manager{Terminal: interactive(streams)}
				if e := manager.Apply(ctx, action, authority); e != nil {
					return nil, e
				}
				return map[string]any{"item": map[string]any{"fingerprint": authority.Fingerprint(), "trust_action": action, "client_restart_may_be_required": true}, "changed": true}, nil
			})
		}
		a.commands = append(a.commands, c)
	}
}
