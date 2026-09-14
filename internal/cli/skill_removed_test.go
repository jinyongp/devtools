package cli

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestSkillCommandRemoved(t *testing.T) {
	a := New("test", "test")
	code, out, diagnostic := invoke(t, a, "", "skill")
	if code != 2 || out != "" || !json.Valid([]byte(diagnostic)) || !strings.Contains(diagnostic, `"code":"invalid_argument"`) {
		t.Fatalf("removed command: %d %q %q", code, out, diagnostic)
	}

	for _, args := range [][]string{nil, {"schema"}, {"schema", "--all"}, {"completion", "bash"}} {
		code, out, diagnostic = invoke(t, a, "", args...)
		if code != 0 || diagnostic != "" || strings.Contains(out, "skill") {
			t.Fatalf("%v exposes removed command: %d %q %q", args, code, out, diagnostic)
		}
	}
}
