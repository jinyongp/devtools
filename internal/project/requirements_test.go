package project

import "testing"

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
