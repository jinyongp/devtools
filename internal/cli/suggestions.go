package cli

import (
	"github.com/jinyongp/devtools/internal/protocol"
	"sort"
	"strings"
)

func suggestions(input string, candidates []string) []string {
	type match struct {
		name     string
		distance int
	}
	matches := []match{}
	a := []rune(strings.ToLower(input))
	for _, name := range candidates {
		b := []rune(strings.ToLower(name))
		difference := len(a) - len(b)
		if difference > 2 || difference < -2 {
			continue
		}
		row := make([]int, len(b)+1)
		for j := range row {
			row[j] = j
		}
		for i, r := range a {
			next := make([]int, len(b)+1)
			next[0] = i + 1
			for j, s := range b {
				cost := 0
				if r != s {
					cost = 1
				}
				next[j+1] = min(next[j]+1, row[j+1]+1, row[j]+cost)
			}
			row = next
		}
		if d := row[len(b)]; d <= 2 {
			matches = append(matches, match{name, d})
		}
	}
	sort.Slice(matches, func(i, j int) bool {
		if matches[i].distance == matches[j].distance {
			return matches[i].name < matches[j].name
		}
		return matches[i].distance < matches[j].distance
	})
	out := []string{}
	for _, m := range matches {
		if len(out) == 3 {
			break
		}
		out = append(out, m.name)
	}
	return out
}
func (a *App) unknownCommand(args []string) *protocol.Error {
	group := []string{}
	candidates := []string{}
	token := ""
	index := 0
	for i, part := range args {
		index = i
		children := map[string]bool{}
		prefix := strings.Join(group, " ")
		if prefix != "" {
			prefix += " "
		}
		for _, c := range a.commands {
			for _, name := range append([]string{c.Name}, c.Aliases...) {
				if strings.HasPrefix(name, prefix) && name != strings.TrimSpace(prefix) {
					children[strings.Split(strings.TrimPrefix(name, prefix), " ")[0]] = true
				}
			}
		}
		if !children[part] {
			token = part
			for name := range children {
				candidates = append(candidates, name)
			}
			break
		}
		group = append(group, part)
		if i == len(args)-1 {
			token = part
		}
	}
	argv := append([]string{"devtools", "help"}, group...)
	return protocol.NewError("invalid_argument", "Unknown command. Use the scoped help or a suggested command.", 2, map[string]any{"phase": "parse", "reason": "unknown_command", "target": map[string]any{"kind": "command", "name": token}, "sources": []any{map[string]any{"kind": "argv", "path": []string{"command"}, "offset": index}}, "facts": map[string]any{"suggestions": suggestions(token, candidates)}, "remedies": []protocol.Remedy{{Argv: argv, RequiredInputs: []string{}, Message: "List commands in this group.", Effect: "read"}}})
}
