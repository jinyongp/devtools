package cli

import (
	"fmt"
	"sort"
	"strings"
)

type helpSection struct {
	title string
	names string
}

// Human navigation follows tasks rather than the machine catalog's sort order.
// Unlisted commands remain visible so adding a command cannot hide it from help.
var helpSections = map[string][]helpSection{
	"": {
		{"Project & execution", "init project instance command run process"},
		{"Values & protection", "profile env variable secret bundle import vault values"},
		{"Networking & sharing", "port proxy expose"},
		{"Tasks & coordination", "task"},
		{"Storage & recovery", "backup cleanup"},
		{"Inspect & maintain", "dashboard doctor diagnostics version update completion help schema"},
	},
	"task": {
		{"Plan & inspect", "list show add update next tree depends impact"},
		{"Run & recover", "claim current context checkpoint sync release unclaim takeover"},
		{"Pause & finish", "hold unhold resume done cancel reopen"},
		{"Organize & record", "attach detach workstream validation history export"},
	},
	"task workstream": {
		{"Plan & inspect", "list show create update spec plan edit depends tree impact context check"},
		{"Lifecycle", "activate close cancel reopen"},
		{"Records", "history export"},
	},
	"task validation": {
		{"Define", "list show add update"},
		{"Validate", "basis record accept"},
		{"Exceptions", "waive unwaive"},
	},
	"proxy": {
		{"Routes", "list env add up down rm clear"},
		{"Listener lifecycle", "status logs configure setup start restart stop teardown"},
		{"Foreground execution", "run"},
		{"Certificates & hosts", "ca trust untrust hosts"},
	},
	"values": {
		{"Inspect & edit", "inspect uses revision request"},
		{"Approved execution", "runner execution"},
	},
}

var helpOrders = map[string]string{
	"backup":           "status keygen configure create inspect restore",
	"cleanup":          "archives preview apply restore purge",
	"command":          "list inspect env run",
	"profile":          "list inspect diff export import transfer",
	"proxy hosts":      "preview apply remove",
	"secret":           "list set generate unset",
	"values execution": "list status prepare approve deliver report cancel",
	"values runner":    "address show register unregister",
	"vault":            "status init unlock lock recover",
}

var helpAliases = map[string]string{"cmd": "command", "var": "variable", "sec": "secret"}

func helpScope(target string) string {
	words := strings.Fields(target)
	if len(words) > 0 {
		if name, ok := helpAliases[words[0]]; ok {
			words[0] = name
		}
	}
	return strings.Join(words, " ")
}

func orderedHelpNames(scope string, names []string) []string {
	order := helpOrders[scope]
	if order == "" {
		order = "list show inspect status check logs env preview create init configure set add allocate name move rename prepare start up run approve deliver report wait restart stop down unset remove release prune purge restore recover export import"
	}
	rank := map[string]int{}
	for i, name := range strings.Fields(order) {
		rank[name] = i + 1
	}
	result := append([]string(nil), names...)
	sort.Slice(result, func(i, j int) bool {
		a, b := rank[result[i]], rank[result[j]]
		if a == 0 {
			a = len(rank) + 1
		}
		if b == 0 {
			b = len(rank) + 1
		}
		if a != b {
			return a < b
		}
		return result[i] < result[j]
	})
	return result
}

func (a *App) commandHelp(target string, names []string) string {
	scope := helpScope(target)
	remaining := map[string]bool{}
	labels := map[string]string{}
	for _, name := range names {
		remaining[name], labels[name] = true, name
	}
	if target == "" {
		for alias, canonical := range helpAliases {
			if remaining[alias] && remaining[canonical] {
				delete(remaining, alias)
				labels[canonical] = canonical + " (" + alias + ")"
			}
		}
	}
	width := 0
	for name := range remaining {
		if len(labels[name]) > width {
			width = len(labels[name])
		}
	}
	var b strings.Builder
	b.WriteString("Commands:\n")
	write := func(name string) {
		full := strings.TrimSpace(target + " " + name)
		fmt.Fprintf(&b, "  %-*s  %s\n", width, labels[name], a.commandSummary(full))
		delete(remaining, name)
	}
	sections := helpSections[scope]
	for _, section := range sections {
		selected := []string{}
		for _, name := range strings.Fields(section.names) {
			if remaining[name] {
				selected = append(selected, name)
			}
		}
		if len(selected) == 0 {
			continue
		}
		fmt.Fprintf(&b, "\n%s:\n", section.title)
		for _, name := range selected {
			write(name)
		}
	}
	if len(remaining) > 0 {
		if len(sections) > 0 {
			b.WriteString("\nOther commands:\n")
		}
		left := make([]string, 0, len(remaining))
		for name := range remaining {
			left = append(left, name)
		}
		for _, name := range orderedHelpNames(scope, left) {
			write(name)
		}
	}
	return b.String()
}
