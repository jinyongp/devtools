package configcontract

import "testing"

func TestRangeContract(t *testing.T) {
	for _, c := range []struct {
		text  string
		valid bool
	}{
		{"26", true}, {"26.12", true}, {"^26.2.0", true}, {"26 || 28", true}, {"26.2 - 28.1", true},
		{"", false}, {" ", false}, {"26 | 28", false}, {"^bad", false}, {"1.x.2", false}, {"26.1.0\n", false},
	} {
		for _, def := range []string{"Tool", "ToolPatch"} {
			if got := len(Validate(def, map[string]any{"version": c.text})) == 0; got != c.valid {
				t.Errorf("%s %q: %v, want %v", def, c.text, got, c.valid)
			}
		}
	}
}
