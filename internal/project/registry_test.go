package project

import "testing"

func TestProjectSearchRetainsIOCondition(t *testing.T) {
	missing := t.TempDir() + "/missing"
	_, failure := Resolve(missing, "")
	if failure == nil || failure.Code != "io_error" || failure.Details["reason"] != "not_found" {
		t.Fatal("project resolution lost the OS condition", failure)
	}
	_, failure = Find(missing)
	if failure == nil || failure.Code != "io_error" || failure.Details["reason"] != "not_found" {
		t.Fatal("configuration lookup lost the OS condition", failure)
	}
}

func TestRegistrySelectionAndWholeOverride(t *testing.T) {
	d, e := ParseDocument([]byte(`profile = "sample"
[tools]
node = {executable="custom",version="1.2.3",version_args=["-v"]}
go = {}
[presets.web]
tools = ["node"]
vars = ["MODE"]
[defaults]
tools = ["go"]
[commands."dev.web"]
exec = ["node"]
requires = ["web"]
tool_overrides.node = {version="2.0.0"}
`), "devtools.toml", ".")
	if e != nil {
		t.Fatal(e)
	}
	c := d.Context.Commands["dev.web"]
	r := d.Context.Requirements.Merge(c.Requirements)
	if len(r.Tools) != 2 || len(r.Vars) != 1 {
		t.Fatalf("selection not resolved: %#v", r)
	}
	tool := r.Tools["node"].WithDefaults("node")
	if tool.Executable != "node" || tool.VersionArgs[0] != "--version" {
		t.Fatalf("inherited an overridden tool: %#v", tool)
	}
	if d.Format != "registry" {
		t.Fatal(d.Format)
	}
}
func TestRegistryRejectsMixedAndUnknownReferences(t *testing.T) {
	for _, text := range []string{`profile="sample"
[requirements]
[tools]
`, `profile="sample"
[commands.test]
exec=["node"]
tools=["missing"]
`} {
		if _, e := ParseDocument([]byte(text), "devtools.toml", "."); e == nil {
			t.Fatal("accepted invalid registry")
		}
	}
}
