package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/jinyongp/devtools/internal/completion"
	"github.com/jinyongp/devtools/internal/network"
	"github.com/jinyongp/devtools/internal/paths"
	"github.com/jinyongp/devtools/internal/ports"
	"github.com/jinyongp/devtools/internal/project"
	"github.com/jinyongp/devtools/internal/protocol"
	"github.com/jinyongp/devtools/internal/services"
	"github.com/jinyongp/devtools/internal/tasks"
	"github.com/jinyongp/devtools/internal/values"
)

var completionIdentifier = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9._-]{0,255}$`)

type completionCandidate struct{ Value, Description string }

type completionQuery struct {
	Context       context.Context
	Command       *Command
	Options       map[string]string
	Arguments     []string
	Field, Prefix string
	Descriptions  map[string]string
}

// The private shell protocol accepts NUL-delimited words through stdin, including
// the current (possibly empty) token. It never echoes input or diagnostics.
func (a *App) completeDynamic(ctx context.Context, streams IO, describe bool) int {
	if streams.In == nil || ctx.Err() != nil {
		return 0
	}
	if f, ok := streams.In.(*os.File); ok {
		if info, err := f.Stat(); err != nil || info.Mode()&os.ModeCharDevice != 0 {
			return 0
		}
	}
	ctx, cancel := context.WithTimeout(ctx, completion.Budget)
	defer cancel()
	result := make(chan []completionCandidate, 1)
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
		result <- a.completionCandidates(ctx, words, describe)
	}()
	select {
	case <-ctx.Done():
	case candidates := <-result:
		for _, candidate := range candidates {
			line := candidate.Value
			if describe {
				line += "\t" + strings.Join(strings.Fields(candidate.Description), " ")
			}
			if _, err := io.WriteString(streams.Out, line+"\n"); err != nil {
				return 1
			}
		}
	}
	return 0
}

// Parse only enough of the incomplete command to identify its candidate source.
// Execution parsers and handlers are deliberately not called by completion.
func (a *App) dynamicCandidates(words []string) []string {
	items := a.completionCandidates(context.Background(), words, false)
	var out []string
	for _, item := range items {
		out = append(out, item.Value)
	}
	return out
}

func (a *App) completionCandidates(ctx context.Context, words []string, all bool) []completionCandidate {
	if len(words) == 0 {
		return nil
	}
	prefix := words[len(words)-1]
	past := words[:len(words)-1]
	rootEnd := 0
	rootSelection := map[string]string{}
	formatCandidates := func(prefix, replacement string) []completionCandidate {
		out := []completionCandidate{}
		for _, value := range []string{"auto", "text", "json"} {
			if strings.HasPrefix(value, prefix) {
				out = append(out, completionCandidate{Value: replacement + value, Description: "Output format"})
			}
		}
		return out
	}
	for rootEnd < len(past) {
		name, value, inline := strings.Cut(past[rootEnd], "=")
		if name != "--format" && name != "--isolated-root" {
			break
		}
		if _, exists := rootSelection[strings.TrimPrefix(name, "--")]; exists {
			return nil
		}
		rootEnd++
		if !inline {
			if rootEnd == len(past) {
				if name == "--format" {
					return formatCandidates(prefix, "")
				}
				return nil
			}
			value = past[rootEnd]
			rootEnd++
		}
		if value == "" || name == "--format" && value != "auto" && value != "text" && value != "json" {
			return nil
		}
		rootSelection[strings.TrimPrefix(name, "--")] = value
	}
	if rootEnd == len(past) && strings.HasPrefix(prefix, "--format=") {
		if _, exists := rootSelection["format"]; exists {
			return nil
		}
		return formatCandidates(strings.TrimPrefix(prefix, "--format="), "--format=")
	}
	past = past[rootEnd:]
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
		if all {
			return a.completionChildren(past, prefix, rootSelection)
		}
		return nil
	}
	opts := map[string]Option{}
	for _, opt := range cmd.Options {
		opts["--"+opt.Name] = opt
	}
	selected := rootSelection
	values := map[string][]string{}
	pending := ""
	positionals := 0
	arguments := []string{}
	positionalValues := map[string]bool{}
	firstArgument := ""
	for _, word := range past[consumed:] {
		if pending != "" {
			// Bash can split --option=value at its '=' word boundary.
			if word == "=" {
				continue
			}
			if !validOption(opts["--"+pending], word) {
				return nil
			}
			selected[pending] = word
			values[pending] = append(values[pending], word)
			pending = ""
			continue
		}
		if word == "--" {
			return nil
		}
		if strings.HasPrefix(word, "-") {
			if word == "--help" || word == "-h" {
				return nil
			}
			name, value, equal := strings.Cut(word, "=")
			opt, exists := opts[name]
			if !exists {
				return nil
			}
			if _, exists := selected[opt.Name]; exists && !opt.Repeatable {
				return nil
			}
			if equal {
				if opt.Boolean && value != "true" && value != "false" {
					return nil
				}
				if !validOption(opt, value) {
					return nil
				}
				selected[opt.Name] = value
				values[opt.Name] = append(values[opt.Name], value)
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
		if cmd.UniqueArgs && positionalValues[word] {
			return nil
		}
		positionalValues[word] = true
		arguments = append(arguments, word)
		positionals++
		if cmd.Name == "command run" {
			return nil
		}
		if len(cmd.Arguments) == 0 || positionals > len(cmd.Arguments) && !cmd.Arguments[len(cmd.Arguments)-1].Repeatable {
			return nil
		}
		if !validArgument(cmd.Arguments[min(positionals-1, len(cmd.Arguments)-1)], word) {
			return nil
		}
	}
	replacement := ""
	if pending == "" && strings.HasPrefix(prefix, "--") {
		name, value, equal := strings.Cut(prefix, "=")
		opt, exists := opts[name]
		if equal && !exists || !equal && !all {
			return nil
		}
		if equal {
			if _, used := selected[opt.Name]; used && !opt.Repeatable {
				return nil
			}
			pending, prefix, replacement = opt.Name, value, name+"="
		}
	}
	source := completionSource("")
	var choices []string
	query := completionQuery{Context: ctx, Command: cmd, Options: selected, Arguments: arguments, Field: pending, Prefix: prefix, Descriptions: map[string]string{}}
	candidates := []completionCandidate{}
	if all && pending == "" {
		for _, option := range cmd.Options {
			if _, exists := selected[option.Name]; exists && !option.Repeatable {
				continue
			}
			if !completionOptionAllowed(option, selected) {
				continue
			}
			input := make(map[string]string, len(selected)+1)
			for name, value := range selected {
				input[name] = value
			}
			input[option.Name] = "true"
			unknown := ""
			if !option.Boolean {
				unknown = option.Name
			}
			if completionInputPossible(*cmd, input, arguments, unknown) {
				candidates = append(candidates, completionCandidate{Value: "--" + option.Name, Description: option.Description})
			}
		}
		candidates = append(candidates, completionCandidate{Value: "--help", Description: "Show help"}, completionCandidate{Value: "-h", Description: "Show help"})
		if len(past) == consumed {
			candidates = append(candidates, a.completionChildren(past, prefix, selected)...)
		}
	}
	if all && pending == "" && (cmd.Name == "help" || cmd.Name == "schema") {
		for _, child := range a.completionChildren(arguments, prefix, selected) {
			if !strings.HasPrefix(child.Value, "-") {
				candidates = append(candidates, child)
			}
		}
		return filterCompletionCandidates(candidates, prefix)
	}
	if pending != "" {
		opt := opts["--"+pending]
		if !completionOptionAllowed(opt, selected) {
			return nil
		}
		source, choices = opt.Completion, patternChoices(opt.Pattern)
		if opt.Boolean {
			choices = []string{"true", "false"}
		}
		// Restart's selected environment belongs to its recorded execution,
		// never the caller's current project. Without that scope stay silent.
	} else {
		if len(cmd.Arguments) == 0 || strings.HasPrefix(prefix, "-") {
			return filterCompletionCandidates(candidates, prefix)
		}
		index := positionals
		if index >= len(cmd.Arguments) {
			index = len(cmd.Arguments) - 1
			if !cmd.Arguments[index].Repeatable {
				return filterCompletionCandidates(candidates, prefix)
			}
		}
		arg := cmd.Arguments[index]
		source = arg.Completion
		choices = patternChoices(arg.Pattern)
	}
	if source == "" && len(choices) == 0 {
		return filterCompletionCandidates(candidates, prefix)
	}
	// Task --dir selects execution/query locations; profile resolution uses cwd.
	if strings.HasPrefix(cmd.Name, "task ") {
		delete(selected, "dir")
	}
	if root := selected["isolated-root"]; root != "" {
		directories, err := paths.Isolated(root)
		if err != nil {
			return nil
		}
		// Query the selected root without mutating App or creating user paths.
		scoped := *a
		scoped.isolatedRoot = directories.IsolatedRoot
		scoped.dataDirectory = func() (string, *protocol.Error) { return directories.Data + "/profiles", nil }
		a = &scoped
	}
	names := choices
	if source != "" {
		names = a.completionNames(string(source), query)
	}
	for _, name := range names {
		if pending == "depends-on" && name == firstArgument || cmd.UniqueArgs && pending == "" && positionalValues[name] {
			continue
		}
		valid := completionIdentifier.MatchString(name)
		if source == "command" {
			valid = project.ValidCommand(name)
		}
		if source == completeInstance {
			valid, _ = regexp.MatchString(instanceSelectorPattern, name)
		}
		if !valid || !strings.HasPrefix(name, prefix) {
			continue
		}
		input, args := selected, append([]string{}, arguments...)
		if pending != "" {
			duplicate := false
			for _, chosen := range values[pending] {
				duplicate = duplicate || chosen == name
			}
			if duplicate {
				continue
			}
			input = make(map[string]string, len(selected)+1)
			for key, value := range selected {
				input[key] = value
			}
			input[pending] = name
		} else {
			args = append(args, name)
		}
		if completionInputPossible(*cmd, input, args, "") {
			candidates = append(candidates, completionCandidate{Value: replacement + name, Description: query.Descriptions[name]})
		}
	}
	return filterCompletionCandidates(candidates, words[len(words)-1])
}

func completionOptionAllowed(option Option, selected map[string]string) bool {
	return option.Name != "columns" || selected["format"] != "json"
}

func filterCompletionCandidates(items []completionCandidate, prefix string) []completionCandidate {
	sort.SliceStable(items, func(i, j int) bool { return items[i].Value < items[j].Value })
	out := []completionCandidate{}
	for _, item := range items {
		if strings.HasPrefix(item.Value, prefix) && (len(out) == 0 || out[len(out)-1].Value != item.Value) {
			out = append(out, item)
			if len(out) == 100 {
				break
			}
		}
	}
	return out
}

func (a *App) completionChildren(past []string, prefix string, selected map[string]string) []completionCandidate {
	items := []completionCandidate{}
	for _, command := range a.commands {
		for _, name := range append([]string{command.Name}, command.Aliases...) {
			parts := strings.Fields(name)
			if len(parts) > len(past) && strings.Join(parts[:len(past)], " ") == strings.Join(past, " ") {
				path := strings.Join(parts[:len(past)+1], " ")
				items = append(items, completionCandidate{Value: parts[len(past)], Description: a.commandSummary(path)})
			}
		}
	}
	if len(past) == 0 {
		for _, option := range []completionCandidate{{"--format", "Output format"}, {"--isolated-root", "Independent user paths"}, {"--version", "Show version"}, {"--help", "Show help"}, {"-h", "Show help"}} {
			if _, exists := selected[strings.TrimPrefix(option.Value, "--")]; !exists {
				items = append(items, option)
			}
		}
	}
	return filterCompletionCandidates(items, prefix)
}

func (a *App) completionNames(source string, query completionQuery) []string {
	selected := query.Options
	if source == "process" {
		store, e := a.serviceStore()
		if e != nil {
			return nil
		}
		items, e := store.CompletionRecords(query.Context, services.CompletionSelection{Action: strings.TrimPrefix(query.Command.Name, "process "), Prefix: query.Prefix})
		if e != nil {
			return nil
		}
		names := []string{}
		for _, item := range items {
			names = append(names, item.ID)
			query.Descriptions[item.ID] = fmt.Sprintf("%s (%s, %s; %s)", item.Command, item.Mode, item.State, item.Profile)
		}
		return names
	}
	if source == "listener" {
		manager, condition := a.proxyStore()
		if condition != nil {
			return nil
		}
		names, _ := manager.CompletionListeners(query.Context, query.Command.Name == "proxy stop", query.Prefix)
		return names
	}
	if source == "route" && (selected["global"] == "true" || query.Command.Name == "proxy rm") {
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
			for _, def := range st.Network.Routes {
				if completionRouteAllowed(query.Command.Name, st, def.Ref()) {
					names = append(names, def.Name)
				}
			}
		}
		return names
	}
	if source == "exposure" {
		m, e := a.proxyStore()
		if e != nil {
			return nil
		}
		items, e := m.CompletionExposures(query.Context, query.Command.Name == "expose stop", query.Prefix)
		if e != nil {
			return nil
		}
		names := []string{}
		for _, item := range items {
			names = append(names, item.ID)
			query.Descriptions[item.ID] = item.Host + " (" + item.State + ")"
		}
		return names
	}
	dir := selected["dir"]
	if dir == "" {
		dir = "."
	}
	if source == "instance" {
		// A move's --dir is its destination, not the profile owning its source.
		p, e := a.resolveStoredProject(".", selected["profile"])
		if e != nil {
			return nil
		}
		store, e := a.portStore()
		if e != nil {
			return nil
		}
		state, e := store.Read()
		if e != nil {
			return nil
		}
		names := []string{}
		for _, item := range state.Instances {
			if item.Profile == p.Profile {
				names = append(names, "id:"+item.ID)
				if item.Alias != nil {
					names = append(names, *item.Alias)
				}
			}
		}
		return names
	}
	if source == "port" || source == "port-declaration" {
		return a.portCompletionNames(selected, source == "port-declaration")
	}
	if source == "command" || source == "route" || source == "tool" || source == "preset" {
		p, e := a.resolveStoredProject(dir, "")
		if e != nil {
			return nil
		}
		if source == "command" && strings.HasPrefix(query.Command.Name, "project ") && query.Command.Name != "project up" {
			store, e := a.serviceStore()
			if e != nil {
				return nil
			}
			action := strings.TrimPrefix(query.Command.Name, "project ")
			if action == "down" {
				action = "stop"
			}
			items, e := store.CompletionRecords(query.Context, services.CompletionSelection{NavigationSelection: services.NavigationSelection{Profile: p.Profile, Directory: p.Root}, Action: action, ActiveOnly: true})
			if e != nil {
				return nil
			}
			names := []string{}
			for _, item := range items {
				names = append(names, item.Command)
			}
			return names
		}
		names := []string{}
		if source == "route" {
			var state *ports.State
			if query.Command.Name == "proxy down" {
				store, condition := a.portStore()
				if condition != nil {
					return nil
				}
				state, condition = store.Read()
				if condition != nil {
					return nil
				}
			}
			for name := range p.Proxies {
				if state != nil {
					if instance := state.Find(p.Profile, "", p.Root); instance != nil && !completionRouteAllowed(query.Command.Name, state, network.RouteRef{Scope: "project", Profile: p.Profile, InstanceID: instance.ID, Proxy: name}) {
						continue
					}
				}
				names = append(names, name)
			}
			return names
		}
		if source == "tool" || source == "preset" {
			if p.Document != nil {
				entries, _ := p.Document.Raw[source+"s"].(map[string]any)
				for name := range entries {
					names = append(names, name)
				}
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
		if catalog, condition := a.profileCatalog(); condition == nil {
			stored, _ := catalog.CompletionNames(query.Context)
			names = append(names, stored...)
		}
		if p, e := a.resolveStoredProject(dir, ""); e == nil {
			names = append(names, p.Profile)
		}
		return names
	}
	p, e := a.resolveStoredProject(dir, selected["profile"])
	if query.Command.Name == "process restart" && (source == "env" || source == "selection") {
		if len(query.Arguments) == 0 {
			return nil
		}
		store, condition := a.serviceStore()
		if condition != nil {
			return nil
		}
		records, condition := store.StoredMetadata()
		if condition != nil {
			return nil
		}
		found := false
		for _, record := range records {
			if record.ID == query.Arguments[0] && !record.DetailRetired {
				p.Profile, e, found = record.Profile, nil, true
				break
			}
		}
		if !found {
			return nil
		}
	}
	if e != nil {
		return nil
	}
	if source == "bundle" || source == "selection" {
		names, _ := (values.ModernStore{Directory: data, Profile: p.Profile}).CompletionBundleNames()
		if source == "selection" {
			names = append(names, "all", "none")
		}
		return names
	}
	if source == "env" || source == "variable" || source == "secret" || source == "value-key" {
		store := values.ModernStore{Directory: data, Profile: p.Profile}
		if source == "value-key" {
			// Bundles contain shared IDs, independent of environment overrides.
			names, _ := store.CompletionValueNames()
			return names
		}
		kind := values.Kind(source)
		if source == "env" {
			kind = ""
		}
		names, _ := store.CompletionNames(kind, selected["env"], strings.HasSuffix(query.Command.Name, " unset"))
		return names
	}
	filter := tasks.CompletionFilter{Action: query.Command.CompletionAction, Field: query.Field, Owner: selected["task"]}
	if len(query.Arguments) > 0 {
		filter.Target = query.Arguments[0]
	}
	names, _ := (tasks.Store{Directory: filepath.Join(filepath.Dir(data), "tasks"), Profile: p.Profile}).CompletionIDsFor(source, selected["workstream"], filter)
	return names
}

func completionRouteAllowed(command string, state *ports.State, ref network.RouteRef) bool {
	activation := state.Activation(ref)
	return command != "proxy down" || activation == nil || activation.Mode != "disabled"
}

func (a *App) portCompletionNames(selected map[string]string, declared bool) []string {
	profile, selector, dir, e := a.portTarget(Request{Options: selected})
	if e != nil {
		return nil
	}
	store, e := a.portStore()
	if e != nil {
		return nil
	}
	state, e := store.Read()
	if e != nil {
		return nil
	}
	instance := state.Find(profile, selector, dir)
	if instance == nil && selector != "" {
		return nil
	}
	names := []string{}
	if instance != nil {
		dir = instance.Directory
		if !declared {
			for _, item := range state.Assignments {
				if item.Profile == profile && item.ID == instance.ID {
					names = append(names, item.Name)
				}
			}
		}
	}
	// Allocation needs declarations from this exact execution directory. Reading
	// or releasing an existing assignment does not require its config to survive.
	if declared {
		if p, e := a.resolveStoredProject(dir, ""); e == nil && p.Profile == profile && p.Root == dir {
			for name := range p.Ports {
				names = append(names, name)
			}
		}
	}
	return names
}
