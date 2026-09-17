package project

import "testing"

func TestToolWithDefaults(t *testing.T) {
	args := []string{"version"}
	tool := Tool{Version: "1.2.3"}
	resolved := tool.WithDefaults("go")
	if resolved.Executable != "go" || len(resolved.VersionArgs) != 1 || resolved.VersionArgs[0] != "--version" {
		t.Fatalf("unexpected defaults: %+v", resolved)
	}
	if tool.Executable != "" || tool.VersionArgs != nil {
		t.Fatalf("mutated source: %+v", tool)
	}
	explicit := Tool{Executable: "./tool", Version: "1.2.3", VersionArgs: args}.WithDefaults("ignored")
	if explicit.Executable != "./tool" || len(explicit.VersionArgs) != 1 || explicit.VersionArgs[0] != "version" {
		t.Fatalf("overrode explicit values: %+v", explicit)
	}
	explicit.VersionArgs[0] = "changed"
	if args[0] != "version" {
		t.Fatal("returned tool shares version args with source")
	}
}

func TestRequirementsConfig(t *testing.T) {
	p, e := parse([]byte(`profile="test"
[requirements]
vars=["PORT"]
secs=["TOKEN"]
[requirements.tools.go]
version="1.27.1"
version_args=["version"]
[commands.test]
exec=["go","test","./..."]
inject=true
[commands.test.requirements]
vars=["MODE","PORT"]
[commands.test.requirements.tools.go]
version="1.27.2"
version_args=["version"]
`), "/project/devtools.toml", "/project")
	if e != nil {
		t.Fatal(e)
	}
	merged := p.Requirements.Merge(p.Commands["test"].Requirements)
	if len(merged.Vars) != 2 || len(merged.Secs) != 1 || merged.Tools["go"].Version != "1.27.2" {
		t.Fatal(merged)
	}
}
func TestInvalidRequirements(t *testing.T) {
	for _, body := range []string{`[requirements.tools.go]
version="^1.27.1"`, `[requirements]
vars=["TOKEN"]
secs=["TOKEN"]`, `[requirements.tools.go]
version_args=["version"]`, `[requirements]
vars=["BAD-KEY"]`, `[requirements.tools.go]
unknown=true`, `[requirements]
vars=["TOKEN"]
[commands.test]
exec=["true"]
[commands.test.requirements]
secs=["TOKEN"]`} {
		if _, e := parse([]byte("profile='test'\n"+body), "config", "root"); e == nil {
			t.Fatal("accepted", body)
		}
	}
}
