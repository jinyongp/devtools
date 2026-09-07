package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
)

func (a *App) discovery(target string, schema bool) (any, string, bool) {
	var exact *Command
	children := map[string]bool{}
	for i := range a.commands {
		c := &a.commands[i]
		for _, name := range append([]string{c.Name}, c.Aliases...) {
			if name == target {
				exact = c
			}
			prefix := target
			if prefix != "" {
				prefix += " "
			}
			if strings.HasPrefix(name, prefix) && name != target {
				children[strings.Split(strings.TrimPrefix(name, prefix), " ")[0]] = true
			}
		}
	}
	if exact != nil {
		if schema {
			catalog := a.catalog()
			for _, entry := range catalog["commands"].([]map[string]any) {
				if entry["name"] != exact.Name {
					continue
				}
				delete(entry, "options")
				delete(entry, "arguments")
				if len(exact.Aliases) == 0 {
					delete(entry, "aliases")
				}
				if !exact.ChildArgs {
					delete(entry, "accepts_child_args")
				}
				if !exact.StreamOutput {
					delete(entry, "stream_output")
				}
				raw, _ := json.Marshal(entry)
				if strings.Contains(string(raw), "#/$defs/") {
					entry["$defs"] = catalog["$defs"]
				}
				return entry, "", true
			}
		}
		var b strings.Builder
		fmt.Fprintf(&b, "Usage: devtools %s", exact.Name)
		for _, arg := range exact.Arguments {
			if arg.Required {
				fmt.Fprintf(&b, " <%s>", arg.Name)
			} else {
				fmt.Fprintf(&b, " [%s]", arg.Name)
			}
		}
		if len(exact.Options) > 0 {
			b.WriteString(" [options]")
		}
		if exact.ChildArgs {
			b.WriteString(" [-- args...]")
		}
		fmt.Fprintf(&b, "\n\n%s\n", exact.Description)
		for _, o := range exact.Options {
			label := "--" + o.Name
			if !o.Boolean {
				label += " VALUE"
			}
			fmt.Fprintf(&b, "  %-24s %s", label, o.Description)
			if o.Required {
				b.WriteString(" (required)")
			}
			if o.Repeatable {
				b.WriteString(" (repeatable)")
			}
			if o.Default != "" {
				fmt.Fprintf(&b, " (default: %s)", o.Default)
			}
			b.WriteByte('\n')
		}
		fmt.Fprintf(&b, "\nJSON schema: devtools schema %s\n", exact.Name)
		return nil, b.String(), true
	}
	if len(children) == 0 {
		return nil, "", false
	}
	names := make([]string, 0, len(children))
	for name := range children {
		names = append(names, name)
	}
	sort.Strings(names)
	if schema {
		return map[string]any{"commands": names, "scope": target, "usage": "devtools schema <command>; --all for full catalog"}, "", true
	}
	heading := "devtools"
	if target != "" {
		heading += " " + target
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%s — developer tools\n\nCommands:\n", heading)
	width := 0
	for _, name := range names {
		if len(name) > width {
			width = len(name)
		}
	}
	for _, name := range names {
		full := strings.TrimSpace(target + " " + name)
		fmt.Fprintf(&b, "  %-*s  %s\n", width, name, a.commandSummary(full))
	}
	fmt.Fprintf(&b, "\nUsage: %s <command> --help\nSchema: devtools schema %s<command>\n", heading, strings.TrimPrefix(heading+" ", "devtools "))
	return nil, b.String(), true
}

func (a *App) commandSummary(name string) string {
	groups := map[string]string{
		"dashboard":               "Open the local management dashboard.",
		"doctor":                  "Check project tools, settings, and required values.",
		"import":                  "Import variables and secrets from a dotenv file.",
		"init":                    "Create devtools.toml for a project.",
		"run":                     "Run a command with project environment values.",
		"schema":                  "Discover command input and output contracts.",
		"skill":                   "Export the bundled agent skill.",
		"update":                  "Update the installed executable.",
		"backup":                  "Back up and restore profile data.",
		"cleanup":                 "Preview and clean up stored data.",
		"env":                     "Manage profile environments.",
		"instance":                "Manage project locations and aliases.",
		"port":                    "Manage local port assignments.",
		"process":                 "Start, inspect, and stop background commands.",
		"project":                 "Inspect project configuration and profile.",
		"secret":                  "Manage secrets without displaying their values.",
		"sec":                     "Manage secrets (alias of secret).",
		"variable":                "Manage readable environment variables.",
		"var":                     "Manage readable variables (alias of variable).",
		"task":                    "Plan, claim, and track project work.",
		"task workstream":         "Manage specifications and coordinated tasks.",
		"task validation":         "Record validation criteria and evidence.",
		"task workstream spec":    "Read and update workstream specifications.",
		"task workstream plan":    "Read and update implementation plans.",
		"task depends":            "Manage task prerequisites.",
		"task workstream depends": "Manage workstream prerequisites.",
	}
	if description, ok := groups[name]; ok {
		return description
	}
	parts := strings.Fields(name)
	if len(parts) == 2 && (parts[0] == "var" || parts[0] == "variable" || parts[0] == "sec" || parts[0] == "secret") {
		descriptions := map[string]string{"get": "Read a variable's effective value.", "list": "List keys and their source; values stay hidden.", "set": "Set a value in the common or selected env scope.", "unset": "Remove a value from the selected scope."}
		if description, ok := descriptions[parts[1]]; ok {
			return description
		}
	}
	for _, c := range a.commands {
		for _, alias := range append([]string{c.Name}, c.Aliases...) {
			if alias == name {
				return c.Description
			}
		}
	}
	return "Explore related commands."
}

func writeHelp(out io.Writer, text string) error { _, err := io.WriteString(out, text); return err }
