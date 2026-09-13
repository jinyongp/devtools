package services

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jinyongp/devtools/internal/maintenance"
	"github.com/jinyongp/devtools/internal/tasks"
)

func TestStaleSupervisorAndRetryConflict(t *testing.T) {
	s := Store{Data: t.TempDir()}
	id := tasks.ID()
	r := Record{ID: id, Profile: "test", Directory: t.TempDir(), State: "running", CreatedAt: time.Now()}
	if e := writePrivate(s.path(id, "record.json"), r); e != nil {
		t.Fatal(e)
	}
	status, e := s.Status(context.Background(), id)
	if e != nil || status.State != "interrupted" {
		t.Fatal(status, e)
	}
	req := Request{Action: "stop", ID: id, RequestID: tasks.ID()}
	result, e := s.Apply(context.Background(), req)
	if e != nil || result.Item.State != "interrupted" {
		t.Fatal(result, e)
	}
	result, e = s.Apply(context.Background(), req)
	if e != nil || !result.Replayed {
		t.Fatal(result, e)
	}
	req.Action = "restart"
	if _, e = s.Apply(context.Background(), req); e == nil || e.Code != "request_conflict" {
		t.Fatal(e)
	}
	if _, e = s.Status(context.Background(), "../../outside"); e == nil || e.Code != "invalid_argument" || e.ExitCode != 2 {
		t.Fatal("invalid ID contract", e)
	}
	if _, e = s.Status(context.Background(), tasks.ID()); e == nil || e.Code != "process_not_found" || e.ExitCode != 3 {
		t.Fatal("missing execution contract", e)
	}
}
func TestBoundedRawLogs(t *testing.T) {
	path := filepath.Join(t.TempDir(), "output.log")
	if e := os.Chmod(filepath.Dir(path), 0700); e != nil {
		t.Fatal(e)
	}
	w := &boundedLog{path: path}
	if _, e := w.Write([]byte(strings.Repeat("a", 2<<20))); e != nil {
		t.Fatal(e)
	}
	w.Write([]byte("end"))
	b, e := maintenance.Read(path, 1<<20)
	if e != nil || len(b) != 1<<20 || !strings.HasSuffix(string(b), "end") {
		t.Fatal(len(b), e)
	}
	i, _ := os.Stat(path)
	if i.Mode().Perm() != 0600 {
		t.Fatal(i.Mode())
	}
}

func TestErrorExitCodes(t *testing.T) {
	for code, want := range map[string]int{"invalid_argument": 2, "process_not_found": 3, "canceled": 130, "execution_failed": 126} {
		if err := failure(code); err.ExitCode != want {
			t.Errorf("%s exit code = %d, want %d", code, err.ExitCode, want)
		}
	}
}
