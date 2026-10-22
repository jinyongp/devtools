package project

import (
	"sort"

	"github.com/jinyongp/devtools/internal/protocol"
)

// ProxySelection matches service declarations without expanding environment or
// reading runtime state. Empty Routes means no routes, never all project routes.
type ProxySelection struct {
	Services []string
	Routes   []string
}

func (p Context) SelectProxies(names []string) (ProxySelection, *protocol.Error) {
	services := map[string]bool{}
	for _, name := range names {
		command, ok := p.Commands[name]
		if !ok {
			return ProxySelection{}, protocol.NewError("command_not_found", "The selected command is not declared.", 3, map[string]any{"command": name})
		}
		for _, service := range command.Serve {
			services[service] = true
		}
	}
	selection := ProxySelection{Services: []string{}, Routes: []string{}}
	for service := range services {
		selection.Services = append(selection.Services, service)
	}
	for name, route := range p.Proxies {
		if services[route.Port] {
			selection.Routes = append(selection.Routes, name)
		}
	}
	sort.Strings(selection.Services)
	sort.Strings(selection.Routes)
	return selection, nil
}
