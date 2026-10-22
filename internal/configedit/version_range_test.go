package configedit

import (
	"context"
	"os"
	"testing"
)

func TestRangeEditingAndAtomicConflict(t *testing.T) {
	path, rev := fixture(t, "profile='sample'\n[tools]\na={executable='node',version='26'}\nb={executable='node',version='>=26.2 <27'}\n[commands.check]\nexec=['node']\ntools=['a','b']\n")
	result := edit(t, path, rev, false, Operation{Op: "tool.update", Name: "a", Value: map[string]any{"version": "26 || 28"}})
	before, _ := os.ReadFile(path)
	for _, r := range []string{"26 | 28", "28"} {
		_, err := Evaluate(context.Background(), path, Input{Revision: result.Revision, Operations: []Operation{{Op: "tool.update", Name: "a", Value: map[string]any{"version": r}}}})
		if err == nil {
			t.Fatalf("accepted %q", r)
		}
		after, _ := os.ReadFile(path)
		if string(after) != string(before) {
			t.Fatalf("invalid range %q changed file", r)
		}
	}
}
