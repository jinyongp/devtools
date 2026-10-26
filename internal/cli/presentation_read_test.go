package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jinyongp/devtools/internal/cli/presentation"
)

func TestReadPresentationUsesRealCommandAndAlias(t *testing.T) {
	root := t.TempDir()
	config := "profile = 'presentation-fixture'\n[commands.dev]\nexec = ['go', 'test']\ninject = false\n"
	if err := os.WriteFile(filepath.Join(root, "devtools.toml"), []byte(config), 0600); err != nil {
		t.Fatal(err)
	}
	for _, command := range []string{"command", "cmd"} {
		a := presentationApp(presentation.Environment{Stdout: presentation.Terminal{Width: 80}})
		exit, out, diagnostic := invoke(t, a, "", "--format", "text", command, "list", "--dir", root)
		if exit != 0 || diagnostic != "" {
			t.Fatal(exit, out, diagnostic)
		}
		for _, expected := range []string{"Commands (1)", "presentation-fixture", "dev", "Off", `"go" "test"`} {
			if !strings.Contains(out, expected) {
				t.Errorf("missing %q: %s", expected, out)
			}
		}
	}
}

func TestReadViewRequiredPathsMatchDeclaredProviderShapes(t *testing.T) {
	app := New("test", "abc")
	for _, command := range app.commands {
		definition := presentation.DefinitionFor(command.Name)
		for _, required := range definition.Required {
			schema := command.Output
			known := true
			if required.Path != "" {
				for _, word := range strings.Split(required.Path, ".") {
					if word == "*" {
						schema, known = schema["items"].(map[string]any)
					} else {
						properties, declared := schema["properties"].(map[string]any)
						if !declared {
							known = false // Open objects and $refs have separate fixtures.
							break
						}
						schema, known = properties[word].(map[string]any)
						if !known {
							t.Errorf("%s view requires undeclared path %s", command.Name, required.Path)
						}
					}
					if !known {
						break
					}
				}
			}
			if !known {
				continue
			}
			kind, _ := schema["type"].(string)
			if kind == "" {
				continue // enum/union constraints retain their independent provider tests.
			}
			want := required.Type
			if want == "strings" {
				want = "array"
			}
			if kind != want && !(want == "number" && kind == "integer") {
				t.Errorf("%s view requires %s:%s; provider declares %s", command.Name, required.Path, required.Type, kind)
			}
		}
	}
}
