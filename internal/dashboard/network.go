package dashboard

import (
	"context"
	"path/filepath"

	"github.com/jinyongp/devtools/internal/ca"
	"github.com/jinyongp/devtools/internal/expose"
	"github.com/jinyongp/devtools/internal/paths"
	"github.com/jinyongp/devtools/internal/ports"
	"github.com/jinyongp/devtools/internal/project"
	"github.com/jinyongp/devtools/internal/protocol"
	"github.com/jinyongp/devtools/internal/proxy"
	"github.com/jinyongp/devtools/internal/truststore"
)

func (s *Server) networkManager() proxy.Manager {
	d, _ := paths.Current()
	return proxy.Manager{Data: filepath.Dir(s.data), IsolatedRoot: d.IsolatedRoot}
}

func (s *Server) networkView(ctx context.Context, profile string) (any, *protocol.Error) {
	if !project.ValidProfile(profile) {
		return nil, protocol.NewError("invalid_argument", "Select a profile.", 2, nil)
	}
	m := s.networkManager()
	state := struct {
		Listeners []map[string]any `json:"listeners"`
		Routes    []proxy.Item     `json:"-"`
		Exposures []expose.Public  `json:"exposures"`
		CA        ca.Status        `json:"ca"`
		Trusted   bool             `json:"trusted"`
		Isolated  bool             `json:"isolated"`
	}{Isolated: m.IsolatedRoot != ""}
	var e *protocol.Error
	state.Listeners, e = m.NetworkStatuses(ctx)
	if e != nil {
		return nil, e
	}
	state.Routes, e = proxy.NewResolver(proxy.PortStore(m)).List(ctx, "")
	if e != nil {
		return nil, e
	}
	state.Exposures, e = m.ExposureList(ctx)
	if e != nil {
		return nil, e
	}
	state.CA, e = (ca.Store{Data: m.Data}).Status()
	if e != nil {
		return nil, e
	}
	if state.CA.Present {
		authority, err := (ca.Store{Data: m.Data}).Read()
		if err != nil {
			return nil, err
		}
		state.Trusted = truststore.Trusted(authority)
	}
	st, e := proxy.PortStore(m).Read()
	if e != nil {
		return nil, e
	}
	routes := []map[string]any{}
	for _, route := range state.Routes {
		if route.Kind != "route" || route.Scope != "global" && route.Profile != profile {
			continue
		}
		route.HostPolicy = route.HostPolicy.Effective()
		var endpoint ports.RouteEndpoint
		var err *protocol.Error
		if route.Scope == "global" {
			if st.Network != nil {
				for _, def := range st.Network.Routes {
					if def.ID == route.RouteID {
						endpoint, err = st.GlobalEndpoint(def)
						break
					}
				}
			}
		} else if route.Proxy != nil {
			p, failure := project.Resolve(route.Directory, "")
			if failure != nil {
				err = failure
			} else if instance := st.Find(route.Profile, "id:"+route.InstanceID, ""); instance != nil {
				endpoint, err = st.ProjectEndpoint(p, *instance, *route.Proxy)
			}
		}
		item := map[string]any{"route": route, "endpoint": endpoint}
		if err != nil {
			item["error_code"] = err.Code
		}
		routes = append(routes, item)
	}
	state.Routes = nil
	selected := state.Exposures[:0]
	for _, item := range state.Exposures {
		if item.Route.Scope == "global" || item.Route.Profile == profile {
			selected = append(selected, item)
		}
	}
	state.Exposures = selected
	return map[string]any{"state": state, "routes": routes}, nil
}

type networkRequest struct {
	Action    string `json:"action"`
	Listener  string `json:"listener,omitempty"`
	ID        string `json:"id,omitempty"`
	RequestID string `json:"request_id"`
}

func (s *Server) networkAction(ctx context.Context, q networkRequest) (any, *protocol.Error) {
	if !uuidPattern.MatchString(q.RequestID) {
		return nil, protocol.NewError("invalid_argument", "Provide a request UUID.", 2, nil)
	}
	m := s.networkManager()
	if q.Action == "exposure.stop" && uuidPattern.MatchString(q.ID) && q.Listener == "" {
		return m.ExposeStop(ctx, q.ID, q.RequestID)
	}
	if (q.Action == "start" || q.Action == "stop" || q.Action == "restart") && project.ValidProfile(q.Listener) && q.ID == "" {
		return m.NetworkApply(ctx, q.Action, q.Listener, false, q.RequestID)
	}
	return nil, protocol.NewError("invalid_argument", "Select a listener action or exposure stop. DNS preparation and OS setup use the initiating CLI.", 2, nil)
}
