package cli

import (
	"github.com/jinyongp/devtools/skills"
	"strings"
	"testing"
)

func TestSkillExport(t *testing.T) {
	code, out, err := invoke(t, New("test", "test"), "", "skill")
	if code != 0 || err != "" || out != skills.Devtools || !strings.HasPrefix(out, "---\nname: devtools\n") {
		t.Fatalf("invalid export: %d %s", code, err)
	}
	code, _, _ = invoke(t, New("test", "test"), "", "skill", "unexpected")
	if code != 2 {
		t.Fatal("unexpected arguments accepted")
	}
}
