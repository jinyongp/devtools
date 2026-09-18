// Package execution resolves project commands into effective execution plans.
package execution

import (
	"github.com/jinyongp/devtools/internal/project"
	"github.com/jinyongp/devtools/internal/protocol"
)

// Command is the immutable command-level input shared by foreground and managed
// execution paths before storage, ports, or process lifetime are involved.
type Command struct {
	Project      project.Context
	Name         string
	Exec         []string
	ExtraArgs    []string
	Directory    string
	Env          string
	Inject       bool
	Requirements project.Requirements
	Ready        *project.ReadyProbe
	Serve        []string
	Bind         map[string]project.Binding
}

// ResolveConfigured derives the effective settings for one named project
// command. An explicit environment selection enables value injection, matching
// the existing CLI and managed-process contracts.
func ResolveConfigured(p project.Context, name string, envOverride *string, extraArgs []string) (Command, *protocol.Error) {
	definition, ok := p.Commands[name]
	if !ok {
		return Command{}, protocol.NewError("command_not_found", "The selected project command is not defined.", 3, map[string]any{"command": name})
	}
	env := definition.Env
	inject := definition.Inject
	if envOverride != nil {
		env = *envOverride
		inject = true
	}
	return Command{
		Project:      p,
		Name:         name,
		Exec:         append([]string{}, definition.Exec...),
		ExtraArgs:    append([]string{}, extraArgs...),
		Directory:    p.Root,
		Env:          env,
		Inject:       inject,
		Requirements: p.Requirements.Merge(definition.Requirements),
		Ready:        cloneReady(definition.Ready),
		Serve:        append([]string{}, definition.Serve...),
		Bind:         cloneBindings(definition.Bind),
	}, nil
}

// ResolveDirect describes a caller-supplied executable. Direct commands inject
// the selected profile values and run from the caller-selected directory.
func ResolveDirect(p project.Context, args []string, directory, env string) (Command, *protocol.Error) {
	if len(args) == 0 || args[0] == "" {
		return Command{}, protocol.NewError("invalid_argument", "Supply an executable after --.", 2, map[string]any{"field": "command"})
	}
	return Command{
		Project:   p,
		Exec:      append([]string{}, args...),
		Directory: directory,
		Env:       env,
		Inject:    true,
	}, nil
}

// Arguments returns the configured executable arguments followed by caller
// arguments. Port/binding expansion intentionally applies only to Exec.
func (c Command) Arguments() []string {
	return append(append([]string{}, c.Exec...), c.ExtraArgs...)
}

func (c Command) projectCommand() project.Command {
	return project.Command{
		Ready:        cloneReady(c.Ready),
		Serve:        append([]string{}, c.Serve...),
		Bind:         cloneBindings(c.Bind),
		Requirements: c.Requirements,
		Exec:         append([]string{}, c.Exec...),
		Inject:       c.Inject,
		Env:          c.Env,
	}
}

func cloneReady(ready *project.ReadyProbe) *project.ReadyProbe {
	if ready == nil {
		return nil
	}
	cloned := *ready
	cloned.Exec = append([]string{}, ready.Exec...)
	return &cloned
}

func cloneBindings(bindings map[string]project.Binding) map[string]project.Binding {
	if bindings == nil {
		return nil
	}
	cloned := make(map[string]project.Binding, len(bindings))
	for key, binding := range bindings {
		if binding.Template != nil {
			template := *binding.Template
			binding.Template = &template
		}
		cloned[key] = binding
	}
	return cloned
}
