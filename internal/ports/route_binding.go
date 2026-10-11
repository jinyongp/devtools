package ports

import (
	"github.com/jinyongp/devtools/internal/network"
	"github.com/jinyongp/devtools/internal/project"
	"github.com/jinyongp/devtools/internal/protocol"
)

// RouteEndpoint calculates configured URLs without starting a daemon, preparing
// DNS, issuing certificates, or importing the proxy data plane into ports.
type RouteEndpoint struct {
	Ref      network.RouteRef `json:"route_ref"`
	Host     string           `json:"host"`
	Listener string           `json:"listener"`
	Port     int              `json:"port"`
	URL      string           `json:"url"`
	RouteURL string           `json:"route_url"`
	Status   string           `json:"status"`
}

func (st *State) ProjectEndpoint(p project.Context, i Instance, name string) (RouteEndpoint, *protocol.Error) {
	def, ok := p.Proxies[name]
	if !ok {
		return RouteEndpoint{}, fail("route_not_found")
	}
	ref := network.RouteRef{Scope: "project", Profile: i.Profile, InstanceID: i.ID, Proxy: name}
	endpoint := RouteEndpoint{Ref: ref, Listener: network.ListenerName(def.Listener), Status: "configured"}
	activation := st.Activation(ref)
	backed := project.NetworkEnvironmentBacked(def.Host)
	if expression := p.Ports[def.Port].Expression; expression != nil && project.NetworkEnvironmentBacked(*expression) {
		backed = true
	}
	if backed {
		if activation == nil || activation.Mode != "active" {
			return endpoint, fail("activation_required")
		}
		endpoint.Host = activation.Host
		endpoint.Listener = activation.Listener
		source, e := p.NetworkSources()
		if e != nil {
			return endpoint, e
		}
		if source != activation.SourceDigest {
			endpoint.Status = "activation_required"
		}
	} else {
		var e *protocol.Error
		endpoint.Host, e = project.Expand(def.Host, func(key string) (string, bool) {
			switch key {
			case "profile":
				return i.Profile, true
			case "proxy":
				return name, true
			case "instance.alias":
				if i.Alias != nil {
					return *i.Alias, true
				}
			}
			return "", false
		})
		if e != nil {
			return endpoint, e
		}
	}
	if !network.ValidHost(endpoint.Host) {
		return endpoint, fail("host_invalid")
	}
	if activation != nil && activation.Mode == "disabled" {
		endpoint.Status = "inactive"
	}
	if assignment := st.Get(i.ID, def.Port); assignment != nil {
		endpoint.Port = assignment.Port
		endpoint.URL = network.BackendURL(assignment.Port)
	} else {
		return endpoint, fail("port_not_found")
	}
	h, t, e := st.ListenerPorts(endpoint.Listener)
	if e != nil {
		return endpoint, e
	}
	endpoint.RouteURL, _ = network.RouteURL(endpoint.Host, h, t)
	if endpoint.RouteURL == "" {
		return endpoint, fail("listener_not_found")
	}
	return endpoint, nil
}
func (st *State) GlobalEndpoint(route network.GlobalRoute) (RouteEndpoint, *protocol.Error) {
	endpoint := RouteEndpoint{Ref: route.Ref(), Host: route.Host, Listener: route.Listener, Status: "inactive"}
	if a := st.Activation(route.Ref()); a != nil && a.Mode == "active" {
		endpoint.Status = "configured"
	}
	if reservation := st.Reservation(route.Reservation); reservation != nil {
		endpoint.Port = reservation.Port
		endpoint.URL = network.BackendURL(reservation.Port)
	} else {
		return endpoint, fail("port_not_found")
	}
	h, t, e := st.ListenerPorts(route.Listener)
	if e != nil {
		return endpoint, e
	}
	endpoint.RouteURL, _ = network.RouteURL(route.Host, h, t)
	if endpoint.RouteURL == "" {
		return endpoint, fail("listener_not_found")
	}
	return endpoint, nil
}
func (st *State) EndpointConflict(selected RouteEndpoint) bool {
	for _, instance := range st.Instances {
		p, e := project.Resolve(instance.Directory, "")
		if e != nil || p.Root != instance.Directory || p.Profile != instance.Profile {
			continue
		}
		for name := range p.Proxies {
			other, e := st.ProjectEndpoint(p, instance, name)
			if e == nil && other.Ref != selected.Ref && other.Host == selected.Host {
				return true
			}
		}
	}
	if st.Network != nil {
		for _, route := range st.Network.Routes {
			if route.Ref() != selected.Ref && route.Host == selected.Host {
				return true
			}
		}
	}
	return false
}
