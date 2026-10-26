package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/jinyongp/devtools/internal/protocol"
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
	names := make([]string, 0, len(children))
	for name := range children {
		names = append(names, name)
	}
	sort.Strings(names)
	if exact != nil {
		if schema {
			catalog := a.catalog()
			for _, entry := range catalog["commands"].([]map[string]any) {
				if entry["name"] != exact.Name {
					continue
				}
				entry["protocol_version"] = catalog["protocol_version"]
				entry["global_options"] = catalog["global_options"]
				delete(entry, "options")
				delete(entry, "arguments")
				if len(exact.Aliases) == 0 {
					delete(entry, "aliases")
				}
				if !exact.ChildArgs {
					delete(entry, "accepts_child_args")
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
		if exact.Name == "command run" && len(exact.Options) > 0 {
			b.WriteString(" [options]")
		}
		for _, arg := range exact.Arguments {
			name := arg.Name
			if arg.Repeatable {
				name += "..."
			}
			if arg.Required {
				fmt.Fprintf(&b, " <%s>", name)
			} else {
				fmt.Fprintf(&b, " [%s]", name)
			}
		}
		if exact.Name != "command run" && len(exact.Options) > 0 {
			b.WriteString(" [options]")
		}
		if exact.ChildArgs {
			b.WriteString(" [args...]")
		}
		fmt.Fprintf(&b, "\n\n%s\n", exact.Description)
		if len(exact.Options) > 0 {
			b.WriteString("\nOptions:\n")
		}
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
			_, facts := inputRule(o.Pattern, o.MinLength, o.MaxLength, 0)
			if choices, ok := facts["allowed"].([]string); ok {
				fmt.Fprintf(&b, " (choices: %s)", strings.Join(choices, ", "))
			}
			if o.Pattern == positiveIntegerPattern {
				b.WriteString(" (positive integer)")
			}
			b.WriteByte('\n')
		}
		if len(names) > 0 {
			b.WriteByte('\n')
			b.WriteString(a.commandHelp(target, names))
		}
		fmt.Fprintf(&b, "\nJSON schema: devtools schema %s\n", exact.Name)
		if len(exact.Examples) > 0 {
			b.WriteString("\nSyntax examples use placeholder IDs/revisions/UUIDs. Replace them with inspected identifiers and one new UUID per new operation.\n")
			b.WriteString("\nExamples:\n")
			for _, example := range exact.Examples {
				fmt.Fprintf(&b, "  %s\n", example)
			}
		}
		if len(exact.Workflow) > 0 {
			b.WriteString("\nWorkflow:\n")
			for i, step := range exact.Workflow {
				fmt.Fprintf(&b, "  %d. %s\n", i+1, step)
			}
		}
		if exact.BodyExample != nil {
			body, _ := json.MarshalIndent(exact.BodyExample, "  ", "  ")
			fmt.Fprintf(&b, "\nJSON input example:\n  %s\n", body)
		}
		return nil, b.String(), true
	}
	if len(children) == 0 {
		return nil, "", false
	}
	if schema {
		return map[string]any{"protocol_version": protocol.ProtocolVersion, "global_options": globalOptions(), "items": names, "scope": target, "usage": "devtools schema <command>; --all for full catalog"}, "", true
	}
	heading := "devtools"
	if target != "" {
		heading += " " + target
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%s — developer tools\n\n", heading)
	b.WriteString(a.commandHelp(target, names))
	fmt.Fprintf(&b, "\nUsage: %s <command> --help\nSchema: devtools schema %s<command>\n", heading, strings.TrimPrefix(heading+" ", "devtools "))
	return nil, b.String(), true
}

func (a *App) commandSummary(name string) string {
	groups := map[string]string{
		"config":                  "Inspect, validate, format, migrate and safely edit devtools.toml.",
		"config tool":             "Register and edit named executable/version specifications.",
		"config preset":           "Register and edit reusable prerequisite selections.",
		"dashboard":               "Open the local management dashboard.",
		"dashboard start":         "Open or reuse the local management dashboard.",
		"dashboard status":        "Inspect the local dashboard server state.",
		"dashboard stop":          "Stop the local dashboard server.",
		"doctor":                  "Check project tools, settings, and required values.",
		"import":                  "Import variables and secrets from a dotenv file.",
		"init":                    "Create devtools.toml for a project.",
		"run":                     "Run a command with project environment values.",
		"schema":                  "Discover command input and output contracts.",
		"update":                  "Update the installed executable.",
		"backup":                  "Back up and restore profile data.",
		"cleanup":                 "Preview and clean up stored data.",
		"bundle":                  "Group values for scoped execution.",
		"values":                  "Inspect value inputs and approve external execution.",
		"vault":                   "Configure and control local key protection.",
		"expose":                  "Share local endpoints through exposure providers.",
		"values execution":        "Prepare, approve, deliver, and report external execution inputs.",
		"values runner":           "Register and inspect same-user execution receivers.",
		"values request":          "Inspect saved value mutation results.",
		"proxy ca":                "Manage the local certificate authority.",
		"proxy hosts":             "Preview and apply local hostname mappings.",
		"profile transfer":        "Prepare encrypted profile transfers.",
		"command":                 "List, inspect, and run configured project commands.",
		"cmd":                     "List, inspect, and run configured project commands (alias of command).",
		"env":                     "Manage profile environments.",
		"instance":                "Manage project locations and aliases.",
		"port":                    "Manage local port assignments.",
		"process":                 "Start, inspect, and stop background commands.",
		"proxy":                   "Route local development servers through a reverse proxy.",
		"project":                 "Inspect project configuration and profile.",
		"profile":                 "Export and import encrypted profile state.",
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
