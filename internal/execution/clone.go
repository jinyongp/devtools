package execution

import (
	"maps"
	"slices"

	"github.com/jinyongp/devtools/internal/network"
	"github.com/jinyongp/devtools/internal/project"
)

func cloneRequirements(r project.Requirements) project.Requirements {
	r.Vars = slices.Clone(r.Vars)
	r.Secs = slices.Clone(r.Secs)
	r.Tools = maps.Clone(r.Tools)
	for name, tool := range r.Tools {
		tool.VersionArgs = append([]string{}, tool.VersionArgs...)
		r.Tools[name] = tool
	}
	return r
}
func cloneDefinition(c project.Command) project.Command {
	c.Exec = append([]string{}, c.Exec...)
	c.Serve = append([]string{}, c.Serve...)
	c.Bind = cloneBindings(c.Bind)
	c.Ready = cloneReady(c.Ready)
	c.Requirements = cloneRequirements(c.Requirements)
	return c
}
func cloneCommand(c Command) Command {
	c.Exec = append([]string{}, c.Exec...)
	c.ExtraArgs = append([]string{}, c.ExtraArgs...)
	c.Serve = append([]string{}, c.Serve...)
	c.Bind = cloneBindings(c.Bind)
	c.Ready = cloneReady(c.Ready)
	c.Requirements = cloneRequirements(c.Requirements)
	c.Project.EnvFiles = append([]string{}, c.Project.EnvFiles...)
	c.Project.Requirements = cloneRequirements(c.Project.Requirements)
	c.Project.Ports = maps.Clone(c.Project.Ports)
	c.Project.Proxies = maps.Clone(c.Project.Proxies)
	c.Project.Commands = maps.Clone(c.Project.Commands)
	for name, port := range c.Project.Ports {
		port.Range = slices.Clone(port.Range)
		if port.Port != nil {
			number := *port.Port
			port.Port = &number
		}
		if port.Expression != nil {
			expression := *port.Expression
			port.Expression = &expression
		}
		c.Project.Ports[name] = port
	}
	for name, command := range c.Project.Commands {
		c.Project.Commands[name] = cloneDefinition(command)
	}
	if c.Network != nil {
		n := *c.Network
		n.Ports = maps.Clone(n.Ports)
		n.Bindings = maps.Clone(n.Bindings)
		n.Listeners = maps.Clone(n.Listeners)
		n.Activations = append([]network.Activation{}, n.Activations...)
		c.Network = &n
	}
	return c
}
