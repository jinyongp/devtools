package cli

import (
	"reflect"
	"sort"
	"strings"
	"testing"
)

func helpRowPosition(t *testing.T, text, name string) int {
	t.Helper()
	for position, line := range strings.Split(text, "\n") {
		fields := strings.Fields(line)
		if strings.HasPrefix(line, "  ") && len(fields) > 0 && fields[0] == name {
			return position
		}
	}
	t.Fatalf("help lost command %q:\n%s", name, text)
	return -1
}

func TestRootHelpGroupsTasksAndKeepsAliasesWithTheirCommands(t *testing.T) {
	a := New("test", "test")
	_, text, ok := a.discovery("", false)
	if !ok {
		t.Fatal("root help unavailable")
	}
	last := -1
	for _, section := range []string{"Project & execution:", "Values & protection:", "Networking & sharing:", "Tasks & coordination:", "Storage & recovery:", "Inspect & maintain:"} {
		position := strings.Index(text, section)
		if position <= last {
			t.Fatalf("root category missing or out of order: %s", section)
		}
		last = position
	}
	for _, label := range []string{"command (cmd)", "variable (var)", "secret (sec)"} {
		if !strings.Contains(text, label) {
			t.Fatalf("missing inline alias %s", label)
		}
	}
	for _, alias := range []string{"cmd", "var", "sec"} {
		for _, line := range strings.Split(text, "\n") {
			fields := strings.Fields(line)
			if len(fields) > 0 && fields[0] == alias {
				t.Fatalf("alias still has a separate root row: %s", alias)
			}
		}
	}
	if strings.Contains(text, "Explore related commands") {
		t.Fatal("root groups have no useful description")
	}
	if helpRowPosition(t, text, "init") >= helpRowPosition(t, text, "run") {
		t.Fatal("setup must precede execution")
	}
}

func TestSubcommandHelpFollowsWorkflowsIncludingAliasScopes(t *testing.T) {
	a := New("test", "test")
	for scope, order := range map[string][]string{
		"command":          {"list", "inspect", "env", "run"},
		"cmd":              {"list", "inspect", "env", "run"},
		"secret":           {"list", "set", "generate", "unset"},
		"sec":              {"list", "set", "generate", "unset"},
		"vault":            {"status", "init", "unlock", "lock", "recover"},
		"proxy hosts":      {"preview", "apply", "remove"},
		"proxy":            {"list", "add", "up", "down", "rm", "status", "start", "restart", "stop", "run", "ca"},
		"values execution": {"prepare", "approve", "deliver", "report"},
		"task validation":  {"basis", "record", "accept"},
		"task workstream":  {"spec", "plan", "check", "activate", "close"},
	} {
		t.Run(scope, func(t *testing.T) {
			_, text, ok := a.discovery(scope, false)
			if !ok {
				t.Fatal("help unavailable")
			}
			previous := -1
			for _, name := range order {
				position := helpRowPosition(t, text, name)
				if position <= previous {
					t.Fatalf("workflow order broken at %s", name)
				}
				previous = position
			}
		})
	}
}

func TestHelpListsEveryChildWithoutChangingSchemaOrdering(t *testing.T) {
	a := New("test", "test")
	scopes := map[string]map[string]bool{}
	for _, command := range a.commands {
		for _, name := range append([]string{command.Name}, command.Aliases...) {
			words := strings.Fields(name)
			for n := range words {
				scope := strings.Join(words[:n], " ")
				if scopes[scope] == nil {
					scopes[scope] = map[string]bool{}
				}
				scopes[scope][words[n]] = true
			}
		}
	}
	for scope, children := range scopes {
		t.Run(scope, func(t *testing.T) {
			before, _, _ := a.discovery(scope, true)
			_, text, ok := a.discovery(scope, false)
			if !ok {
				t.Fatal("help unavailable")
			}
			for child := range children {
				if scope == "" {
					if _, alias := helpAliases[child]; alias {
						continue
					}
				}
				helpRowPosition(t, text, child)
			}
			after, _, _ := a.discovery(scope, true)
			if !reflect.DeepEqual(before, after) {
				t.Fatal("human help mutated schema")
			}
			if group, ok := after.(map[string]any); ok {
				if items, ok := group["items"].([]string); ok && !sort.StringsAreSorted(items) {
					t.Fatal("schema index lost alphabetical order")
				}
			}
			if scope != "" && a.commandSummary(scope) == "Explore related commands." {
				t.Fatal("command group has no useful summary")
			}
		})
	}
}

func TestRunnableGroupHelpExposesChildrenAndFutureCommandsStayVisible(t *testing.T) {
	a := New("test", "test")
	code, text, diagnostic := invoke(t, a, "", "dashboard", "--help")
	if code != 0 || diagnostic != "" || !strings.Contains(text, "Options:") {
		t.Fatal("runnable group help unavailable", code)
	}
	for _, name := range []string{"start", "status", "stop"} {
		helpRowPosition(t, text, name)
	}
	a.commands = append(a.commands, Command{Name: "future inspect", Description: "Inspect future state."})
	_, text, ok := a.discovery("", false)
	if !ok || !strings.Contains(text, "Other commands:") {
		t.Fatal("missing fallback for new commands")
	}
	helpRowPosition(t, text, "future")
}
