package cli

import (
	"strings"
	"testing"
)

func TestQueryAliasesPreservePublicResults(t *testing.T) {
	app := testApp(t)
	for _, words := range [][]string{
		{"env", "create", "local"},
		{"var", "set", "PUBLIC", "--value", "common"},
		{"var", "set", "PUBLIC", "--value", "local", "--env", "local"},
	} {
		if code, _, diagnostic := invoke(t, app, "", append(words, "--profile", "app")...); code != 0 {
			t.Fatal(diagnostic)
		}
	}
	for _, pair := range []struct{ canonical, alias []string }{
		{[]string{"variable", "list"}, []string{"var", "list"}},
		{[]string{"variable", "get", "PUBLIC"}, []string{"var", "get", "PUBLIC"}},
		{[]string{"secret", "list"}, []string{"sec", "list"}},
	} {
		for _, env := range []string{"", "local"} {
			options := []string{"--profile", "app"}
			if env != "" {
				options = append(options, "--env", env)
			}
			code, canonical, diagnostic := invoke(t, app, "", append(pair.canonical, options...)...)
			if code != 0 || diagnostic != "" {
				t.Fatal(diagnostic)
			}
			code, alias, diagnostic := invoke(t, app, "", append(pair.alias, options...)...)
			if code != 0 || diagnostic != "" || alias != canonical || !strings.Contains(alias, `"integrity_verified":false`) {
				t.Fatal("alias or integrity contract changed", diagnostic)
			}
		}
	}
	code, out, diagnostic := invoke(t, app, "", "env", "list", "--profile", "app")
	if code != 0 || diagnostic != "" || !strings.Contains(out, `"local"`) {
		t.Fatal("environment query changed", diagnostic)
	}
}
