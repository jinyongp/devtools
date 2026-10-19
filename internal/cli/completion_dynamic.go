package cli

import (
	"context"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/jinyongp/devtools/internal/project"
	"github.com/jinyongp/devtools/internal/tasks"
	"github.com/jinyongp/devtools/internal/values"
)

var completionIdentifier = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9._-]{0,255}$`)

// The private shell protocol accepts NUL-delimited words through stdin, including
// the current (possibly empty) token. It never echoes input or diagnostics.
func (a *App) completeDynamic(ctx context.Context, streams IO) int {
	if streams.In == nil || ctx.Err() != nil {
		return 0
	}
	if f, ok := streams.In.(*os.File); ok {
		if info, err := f.Stat(); err != nil || info.Mode()&os.ModeCharDevice != 0 {
			return 0
		}
	}
	ctx, cancel := context.WithTimeout(ctx, 200*time.Millisecond)
	defer cancel()
	result := make(chan []string, 1)
	go func() {
		data, err := io.ReadAll(io.LimitReader(streams.In, 65537))
		if err != nil || len(data) == 0 || len(data) > 65536 || data[len(data)-1] != 0 {
			result <- nil
			return
		}
		words := strings.Split(string(data[:len(data)-1]), "\x00")
		if len(words) > 256 {
			result <- nil
			return
		}
		result <- a.dynamicCandidates(words)
	}()
	select {
	case <-ctx.Done():
	case candidates := <-result:
		for _, candidate := range candidates {
			if _, err := io.WriteString(streams.Out, candidate+"\n"); err != nil {
				return 1
			}
		}
	}
	return 0
}

// Parse only enough of the incomplete command to identify its candidate source.
// Execution parsers and handlers are deliberately not called by completion.
func (a *App) dynamicCandidates(words []string) []string {
	if len(words) == 0 {
		return nil
	}
	prefix := words[len(words)-1]
	past := words[:len(words)-1]
	var cmd *Command
	consumed := 0
	for i := range a.commands {
		for _, name := range append([]string{a.commands[i].Name}, a.commands[i].Aliases...) {
			parts := strings.Fields(name)
			if len(parts) > consumed && len(past) >= len(parts) && strings.Join(past[:len(parts)], " ") == name {
				cmd, consumed = &a.commands[i], len(parts)
			}
		}
	}
	if cmd == nil {
		return nil
	}
	opts := map[string]Option{}
	for _, opt := range cmd.Options {
		opts["--"+opt.Name] = opt
	}
	selected := map[string]string{}
	pending := ""
	positionals := 0
	positionalValues := map[string]bool{}
	firstArgument := ""
	for _, word := range past[consumed:] {
		if pending != "" {
			// Bash can split --option=value at its '=' word boundary.
			if word == "=" {
				continue
			}
			selected[pending] = word
			pending = ""
			continue
		}
		if word == "--" {
			return nil
		}
		if strings.HasPrefix(word, "-") {
			name, value, equal := strings.Cut(word, "=")
			opt, exists := opts[name]
			if !exists {
				return nil
			}
			if equal {
				selected[opt.Name] = value
			} else if !opt.Boolean {
				pending = opt.Name
			} else {
				selected[opt.Name] = "true"
			}
			continue
		}
		if positionals == 0 {
			firstArgument = word
		}
		positionalValues[word] = true
		positionals++
		if cmd.Name == "command run" {
			return nil
		}
	}
	replacement := ""
	if pending == "" && strings.HasPrefix(prefix, "--") {
		name, value, equal := strings.Cut(prefix, "=")
		opt, exists := opts[name]
		if !equal || !exists || opt.Boolean {
			return nil
		}
		pending, prefix, replacement = opt.Name, value, name+"="
	}
	source := ""
	switch pending {
	case "listener", "provider", "dns":
		source = pending
	case "profile", "env":
		source = pending
		// Restart inherits the recorded process profile, not the caller's cwd.
		if cmd.Name == "process restart" {
			return nil
		}
	case "workstream":
		if strings.HasPrefix(cmd.Name, "task ") {
			source = "workstream"
		}
	case "task", "task-ids":
		if strings.HasPrefix(cmd.Name, "task ") {
			source = "task"
		}
	case "validation-ids":
		if strings.HasPrefix(cmd.Name, "task ") {
			source = "validation"
		}
	case "depends-on":
		if cmd.Name == "task depends set" {
			source = "task"
		}
		if cmd.Name == "task workstream depends set" {
			source = "workstream"
		}
	case "":
		if len(cmd.Arguments) == 0 || strings.HasPrefix(prefix, "-") {
			return nil
		}
		if positionals != 0 {
			last := len(cmd.Arguments) - 1
			if !cmd.Arguments[last].Repeatable || positionals < last {
				return nil
			}
		}
		switch {
		case cmd.Name == "command run" || cmd.Name == "command inspect" || cmd.Name == "command env" || cmd.Name == "process start" || cmd.Name == "doctor" || strings.HasPrefix(cmd.Name, "project "):
			source = "command"
		case cmd.Name == "expose status" || cmd.Name == "expose stop":
			source = "exposure"
		case cmd.Name == "expose start" || cmd.Name == "proxy env" || cmd.Name == "proxy up" || cmd.Name == "proxy down" || cmd.Name == "proxy rm" || cmd.Name == "proxy run" || strings.HasPrefix(cmd.Name, "proxy hosts "):
			source = "route"
		case cmd.Name == "env remove":
			source = "env"
		case strings.HasPrefix(cmd.Name, "variable "):
			source = "variable"
		case strings.HasPrefix(cmd.Name, "secret "):
			source = "secret"
		case strings.HasPrefix(cmd.Name, "task workstream "):
			source = "workstream"
		case strings.HasPrefix(cmd.Name, "task validation "):
			source = "validation"
		case strings.HasPrefix(cmd.Name, "task "):
			source = "task"
		}
		for _, def := range tasks.Definitions {
			if cmd.Name == "task "+def.Command {
				source = def.Kind
			}
		}
		if cmd.Name == "task checkpoint list" {
			source = "run"
		}
	}
	if source == "" {
		return nil
	}
	// Task --dir selects execution/query locations; profile resolution uses cwd.
	if strings.HasPrefix(cmd.Name, "task ") {
		delete(selected, "dir")
	}
	names := a.completionNames(source, selected)
	sort.Strings(names)
	out := []string{}
	for _, name := range names {
		if pending == "depends-on" && name == firstArgument || cmd.UniqueArgs && pending == "" && positionalValues[name] {
			continue
		}
		valid := completionIdentifier.MatchString(name)
		if source == "command" {
			valid = project.ValidCommand(name)
		}
		if !valid || !strings.HasPrefix(name, prefix) {
			continue
		}
		value := replacement + name
		if len(out) == 0 || out[len(out)-1] != value {
			out = append(out, value)
		}
		if len(out) == 100 {
			break
		}
	}
	return out
}

func (a *App) completionNames(source string, selected map[string]string) []string {
	if source == "provider" {
		return []string{"local", "lan", "cloudflared", "tailscale"}
	}
	if source == "dns" {
		return []string{"auto", "hosts", "external"}
	}
	if source == "listener" || source == "route" && selected["global"] == "true" {
		store, e := a.portStore()
		if e != nil {
			return nil
		}
		st, e := store.Read()
		if e != nil {
			return nil
		}
		names := []string{}
		if st.Network != nil {
			if source == "listener" {
				for _, def := range st.Network.Listeners {
					names = append(names, def.Name)
				}
			} else {
				for _, def := range st.Network.Routes {
					names = append(names, def.Name)
				}
			}
		}
		if source == "listener" && len(names) == 0 {
			names = append(names, "default")
		}
		return names
	}
	if source == "exposure" {
		m, e := a.proxyStore()
		if e != nil {
			return nil
		}
		items, e := m.ExposureList(context.Background())
		if e != nil {
			return nil
		}
		names := []string{}
		for _, item := range items {
			names = append(names, item.ID)
		}
		return names
	}
	dir := selected["dir"]
	if dir == "" {
		dir = "."
	}
	if source == "command" || source == "route" {
		p, e := project.Resolve(dir, "")
		if e != nil {
			return nil
		}
		names := []string{}
		if source == "route" {
			for name := range p.Proxies {
				names = append(names, name)
			}
			return names
		}
		for name := range p.Commands {
			names = append(names, name)
		}
		return names
	}
	data, e := a.dataDirectory()
	if e != nil {
		return nil
	}
	if source == "profile" {
		names := []string{}
		for _, path := range []string{data, filepath.Join(filepath.Dir(data), "tasks")} {
			entries, err := os.ReadDir(path)
			if err != nil {
				continue
			}
			for _, entry := range entries {
				if !entry.Type().IsRegular() || !strings.HasSuffix(entry.Name(), ".json") {
					continue
				}
				decoded, err := hex.DecodeString(strings.TrimSuffix(entry.Name(), ".json"))
				if err == nil && project.ValidProfile(string(decoded)) {
					names = append(names, string(decoded))
				}
			}
		}
		if p, e := project.Resolve(dir, ""); e == nil {
			names = append(names, p.Profile)
		}
		return names
	}
	p, e := project.Resolve(dir, selected["profile"])
	if e != nil {
		return nil
	}
	if source == "env" || source == "variable" || source == "secret" {
		kind := values.Kind(source)
		if source == "env" {
			kind = ""
		}
		names, _ := (values.ModernStore{Directory: data, Profile: p.Profile}).CompletionNames(kind, selected["env"])
		return names
	}
	names, _ := (tasks.Store{Directory: filepath.Join(filepath.Dir(data), "tasks"), Profile: p.Profile}).CompletionIDs(source, selected["workstream"])
	return names
}
