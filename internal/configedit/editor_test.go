package configedit

import (
	"context"
	"github.com/jinyongp/devtools/internal/protocol"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/jinyongp/devtools/internal/projectfile"
)

func fixture(t *testing.T, text string) (string, string) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "devtools.toml")
	if err := os.WriteFile(path, []byte(text), 0640); err != nil {
		t.Fatal(err)
	}
	s, e := projectfile.Read(path)
	if e != nil {
		t.Fatal(e)
	}
	return path, s.Revision
}

func TestConcurrentEditsRequireFreshRevision(t *testing.T) {
	path, revision := fixture(t, "profile='sample'\n")
	runtime := t.TempDir()
	start := make(chan struct{})
	errors := make(chan *protocol.Error, 2)
	var group sync.WaitGroup
	for _, name := range []string{"node", "go"} {
		group.Add(1)
		go func() {
			defer group.Done()
			<-start
			_, e := Evaluate(context.Background(), path, Input{Revision: revision, Runtime: runtime, Operations: []Operation{{Op: "tool.add", Name: name, Value: map[string]any{}}}})
			errors <- e
		}()
	}
	close(start)
	group.Wait()
	close(errors)
	passed, conflicts := 0, 0
	for e := range errors {
		if e == nil {
			passed++
		} else if e.Code == "config_revision_conflict" {
			conflicts++
		} else {
			t.Fatal(e)
		}
	}
	if passed != 1 || conflicts != 1 {
		t.Fatalf("passed=%d conflicts=%d", passed, conflicts)
	}
}

func TestMigrationPreservesAssociatedComments(t *testing.T) {
	path, revision := fixture(t, "profile='sample'\n# shared tools\n[requirements.tools.node] # runtime\nversion='1.2.3' # pinned\n[commands.check] # command\nexec=['node', 'check'] # arguments\n")
	preview, e := Evaluate(context.Background(), path, Input{Migrate: true, DryRun: true})
	if e != nil {
		t.Fatal(e, e.Details)
	}
	result := preview.(MigrationResult)
	_, e = Evaluate(context.Background(), path, Input{Migrate: true, Revision: revision, ApplyDigest: result.ApplyDigest})
	if e != nil {
		t.Fatal(e, e.Details)
	}
	data, _ := os.ReadFile(path)
	for _, comment := range []string{"# shared tools", "# runtime", "# pinned", "# command", "# arguments"} {
		if strings.Count(string(data), comment) != 1 {
			t.Fatal("comment lost or duplicated", comment, string(data))
		}
	}
}
func edit(t *testing.T, path, rev string, dry bool, ops ...Operation) Result {
	t.Helper()
	out, e := Evaluate(context.Background(), path, Input{Revision: rev, DryRun: dry, Operations: ops})
	if e != nil {
		t.Fatalf("%s: %#v", e, e.Details)
	}
	return out.(Result)
}
func TestAtomicForwardReferencesAndRevision(t *testing.T) {
	path, rev := fixture(t, "# heading\r\nprofile = 'sample'\r\n")
	ops := []Operation{{Op: "command.add", Name: "dev.web", Value: map[string]any{"exec": []any{"node", "scripts/dev.mjs"}, "requires": []any{"web"}}}, {Op: "preset.add", Name: "web", Value: map[string]any{"tools": []any{"node"}}}, {Op: "tool.add", Name: "node", Value: map[string]any{}}}
	preview := edit(t, path, rev, true, ops...)
	if preview.Changed || !preview.WouldChange || preview.Revision != rev {
		t.Fatal(preview)
	}
	result := edit(t, path, rev, false, ops...)
	if !result.Changed || result.CandidateDigest != preview.CandidateDigest || result.Revision == rev {
		t.Fatal(result)
	}
	if _, e := Evaluate(context.Background(), path, Input{Revision: rev, Operations: ops}); e == nil || e.Code != "config_revision_conflict" {
		t.Fatal(e)
	}
	data, _ := os.ReadFile(path)
	if !strings.HasPrefix(string(data), "# heading\r\nprofile = 'sample'\r\n") {
		t.Fatal(string(data))
	}
	if strings.Contains(strings.ReplaceAll(string(data), "\r\n", ""), "\n") {
		t.Fatal("mixed inserted newlines")
	}
}
func TestPartialPatchRenameAndNoop(t *testing.T) {
	path, rev := fixture(t, "profile='sample'\n[tools]\nnode = { version = '1.2.3' } # version note\n[commands.\"dev.web\"] # command note\nexec=['node', 'dev'] # argv note\ntools=['node']\n")
	r := edit(t, path, rev, false, Operation{Op: "tool.update", Name: "node", Value: map[string]any{"version": "2.0.0"}})
	r = edit(t, path, r.Revision, false, Operation{Op: "command.rename", Name: "dev.web", NewName: "개발.web"})
	data, _ := os.ReadFile(path)
	if !strings.Contains(string(data), "# version note") || !strings.Contains(string(data), "# command note") || !strings.Contains(string(data), "exec=['node', 'dev'] # argv note") {
		t.Fatal(string(data))
	}
	r = edit(t, path, r.Revision, false, Operation{Op: "command.update", Name: "개발.web", Value: map[string]any{"exec": []any{"node", "dev"}}})
	if r.Changed || r.WouldChange {
		t.Fatal("semantic no-op rewrote source", r)
	}
}
func TestFailuresDoNotPublish(t *testing.T) {
	path, rev := fixture(t, "profile='sample'\n[tools]\nnode={}\n[commands.check]\nexec=['node']\ntools=['node']\n")
	before, _ := os.ReadFile(path)
	for _, ops := range [][]Operation{{{Op: "tool.remove", Name: "node"}}, {{Op: "tool.add", Name: "node", Value: map[string]any{}}}, {{Op: "tool.update", Name: "node", Value: map[string]any{}}}, {{Op: "tool.remove", Name: "node"}, {Op: "tool.add", Name: "node", Value: map[string]any{}}}} {
		if _, e := Evaluate(context.Background(), path, Input{Revision: rev, Operations: ops}); e == nil {
			t.Fatal("accepted invalid edit", ops)
		}
		after, _ := os.ReadFile(path)
		if string(before) != string(after) {
			t.Fatal("partially saved failed batch")
		}
	}
}
func TestMigrationPreservesVariantsAndExecution(t *testing.T) {
	path, rev := fixture(t, `# project
profile='sample'
[requirements.tools]
node={version='1.2.3',executable='custom-node'}
[commands.first]
exec=['node','first']
[commands.first.requirements]
vars=['MODE','MODE']
[commands.first.requirements.tools]
node={version='2.0.0'}
[commands.second]
exec=['node','second']
[commands.second.requirements]
vars=['MODE']
`)
	out, e := Evaluate(context.Background(), path, Input{Revision: rev, Migrate: true, DryRun: true})
	if e != nil {
		t.Fatal(e, e.Details)
	}
	preview := out.(MigrationResult)
	if !preview.Equivalent || preview.Impact.CommandCount != 0 {
		t.Fatal(preview)
	}
	out, e = Evaluate(context.Background(), path, Input{Revision: rev, Migrate: true, ApplyDigest: preview.ApplyDigest})
	if e != nil {
		t.Fatal(e, e.Details)
	}
	result := out.(MigrationResult)
	if !result.Changed || preview.CandidateDigest != result.CandidateDigest {
		t.Fatal(result)
	}
	data, _ := os.ReadFile(path)
	if !strings.Contains(string(data), "shared-") || !strings.Contains(string(data), "tool_overrides") {
		t.Fatal(string(data))
	}
}
func TestFormatIdempotentAndSchema(t *testing.T) {
	path, _ := fixture(t, "profile='sample'\n[commands.check]\nexec=['node'] # preserve\n")
	out, e := Evaluate(context.Background(), path, Input{Format: true})
	if e != nil {
		t.Fatal(e)
	}
	result := out.(Result)
	if !result.Changed {
		t.Fatal(result)
	}
	out, e = Evaluate(context.Background(), path, Input{Format: true})
	if e != nil || out.(Result).WouldChange {
		t.Fatal(out, e)
	}
}

func TestFormatKeepsInterleavedToolTablesLegalAndOrdered(t *testing.T) {
	path, _ := fixture(t, "profile='sample'\n[tools.node]\nversion='1.2.3'\n[commands.check]\nexec=['node']\n[tools.go]\nversion='1.2.3'\n")
	if _, e := Evaluate(context.Background(), path, Input{Format: true}); e != nil {
		t.Fatal(e)
	}
	data, _ := os.ReadFile(path)
	first, command, last := strings.Index(string(data), "node = {"), strings.Index(string(data), "[commands.check]"), strings.Index(string(data), "[tools.go]")
	if first < 0 || command < first || last < command {
		t.Fatal("formatter reordered declarations", string(data))
	}
	if result, e := Evaluate(context.Background(), path, Input{Format: true}); e != nil || result.(Result).WouldChange {
		t.Fatal("format is not idempotent", e)
	}
}

func TestFirstLongToolUsesBlockAndLongNewArrayUsesLines(t *testing.T) {
	path, revision := fixture(t, "profile='sample'\n")
	long := strings.Repeat("argument", 20)
	result := edit(t, path, revision, false,
		Operation{Op: "tool.add", Name: "node", Value: map[string]any{"executable": long}},
		Operation{Op: "command.add", Name: "check", Value: map[string]any{"exec": []any{"node"}}},
	)
	data, err := os.ReadFile(path)
	if err != nil || !strings.Contains(string(data), "[tools.node]") {
		t.Fatal("first long tool was written as an oversized inline table", string(data), err)
	}
	edit(t, path, result.Revision, false, Operation{Op: "command.update", Name: "check", Value: map[string]any{"exec": []any{"node", long}}})
	data, err = os.ReadFile(path)
	if err != nil || !strings.Contains(string(data), "exec = [\n") {
		t.Fatal("new long argv was not split into lines", string(data), err)
	}
}
