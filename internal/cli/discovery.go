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
	return nil, fmt.Sprintf("%s — developer tools\n\nCommands:\n  %s\n\nUsage: %s <command> --help\nSchema: devtools schema <command>\n", heading, strings.Join(names, "\n  "), heading), true
}

func writeHelp(out io.Writer, text string) error { _, err := io.WriteString(out, text); return err }
