package cli

import (
	"os"
	"strings"
	"testing"
)

func TestNavigationInventoryCoversRegistry(t *testing.T) {
	b, err := os.ReadFile("../../docs/cli-navigation-inventory.md")
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]string{}
	kind := ""
	for _, line := range strings.Split(string(b), "\n") {
		if strings.HasPrefix(line, "```navigation-") {
			kind = strings.TrimPrefix(line, "```navigation-")
			continue
		}
		if line == "```" {
			kind = ""
			continue
		}
		if kind == "" || line == "" {
			continue
		}
		if previous := seen[line]; previous != "" {
			t.Errorf("duplicate command %q in %s and %s", line, previous, kind)
		}
		seen[line] = kind
	}
	app := New("test", "test")
	for _, command := range app.commands {
		if seen[command.Name] == "" {
			t.Errorf("unclassified command %q", command.Name)
		}
		delete(seen, command.Name)
	}
	for name := range seen {
		t.Errorf("inventory names unregistered command %q", name)
	}
}
