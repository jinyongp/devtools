package ports

import (
	"sort"
	"strconv"
	"strings"
	"unicode"

	"github.com/jinyongp/devtools/internal/network"
	"github.com/jinyongp/devtools/internal/project"
	"github.com/jinyongp/devtools/internal/protocol"
)

type PendingBinding struct {
	Key  string `json:"key"`
	Code string `json:"code"`
}

func (st *State) PublicBindings(p project.Context, c project.Command) (map[string]string, []PendingBinding) {
	out := map[string]string{}
	pending := []PendingBinding{}
	keys := []string{}
	for key := range c.Bind {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		binding := c.Bind[key]
		if binding.Template != nil {
			continue
		}
		values, e := st.Bind(p, project.Command{Bind: map[string]project.Binding{key: binding}}, "", nil)
		if e != nil {
			pending = append(pending, PendingBinding{key, e.Code})
			continue
		}
		out[key] = values[key]
	}
	for _, key := range keys {
		binding := c.Bind[key]
		if binding.Template == nil {
			continue
		}
		value, e := project.Expand(*binding.Template, func(ref string) (string, bool) {
			if !strings.HasPrefix(ref, "bind.") {
				return "", false
			}
			name := strings.TrimPrefix(ref, "bind.")
			target, ok := c.Bind[name]
			if !ok || target.Template != nil {
				return "", false
			}
			value, ok := out[name]
			return value, ok
		})
		if e != nil {
			pending = append(pending, PendingBinding{key, "public_network_value_unavailable"})
			continue
		}
		out[key] = value
	}
	return out, pending
}
func EnvironmentKeys(env map[string]string) []string {
	keys := make([]string, 0, len(env))
	for key := range env {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
func normalizedService(name string) string {
	var b strings.Builder
	for _, c := range name {
		if c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' {
			b.WriteRune(unicode.ToUpper(c))
		} else {
			b.WriteByte('_')
		}
	}
	return b.String()
}
func (st *State) GlobalEnvironment(selected string) (map[string]string, []PendingBinding, *protocol.Error) {
	out := map[string]string{}
	pending := []PendingBinding{}
	names := map[string]bool{}
	found := false
	if st.Network != nil {
		for _, route := range st.Network.Routes {
			key := normalizedService(route.Name)
			if names[key] {
				return nil, nil, fail("environment_key_conflict")
			}
			names[key] = true
			reservation := st.Reservation(route.Reservation)
			if reservation == nil {
				return nil, nil, fail("port_not_found")
			}
			if route.Name == selected {
				out["PORT"] = strconv.Itoa(reservation.Port)
				found = true
			}
			prefix := "DEVTOOLS_" + key + "_"
			out[prefix+"PORT"] = strconv.Itoa(reservation.Port)
			out[prefix+"URL"] = network.BackendURL(reservation.Port)
			endpoint, e := st.GlobalEndpoint(route)
			if e != nil {
				pending = append(pending, PendingBinding{prefix + "ROUTE_URL", e.Code})
				continue
			}
			out[prefix+"ROUTE_URL"] = endpoint.RouteURL
		}
	}
	if selected != "" && !found {
		return nil, nil, fail("route_not_found")
	}
	return out, pending, nil
}
