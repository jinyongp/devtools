package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jinyongp/devtools/internal/cli/presentation"
	"github.com/jinyongp/devtools/internal/maintenance"
	"github.com/jinyongp/devtools/internal/services"
	"github.com/jinyongp/devtools/internal/tasks"
)

func TestDoctorProgressUsesTheRealLockWithoutChangingMachineOutput(t *testing.T) {
	terminal := presentation.Terminal{IsTerminal: true, ANSI: true, Width: 120}
	for _, tc := range []struct {
		name                      string
		environment               presentation.Environment
		format                    string
		feedback, animate, cancel bool
	}{
		{"terminal", presentation.Environment{Stdout: terminal, Stderr: terminal}, "text", true, true, false},
		{"pipe", presentation.Environment{}, "text", true, false, false},
		{"dumb", presentation.Environment{Stdout: terminal, Stderr: terminal, DumbTerminal: true}, "text", true, false, false},
		{"json_terminal", presentation.Environment{Stdout: terminal, Stderr: terminal}, "json", false, false, false},
		{"agent_text", presentation.Environment{Stdout: terminal, Stderr: terminal, Signals: []string{"AI_AGENT"}}, "text", false, false, false},
		{"canceled_terminal", presentation.Environment{Stdout: terminal, Stderr: terminal}, "text", true, true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			app := testApp(t)
			app.observeOutput = func(IO) presentation.Environment { return tc.environment }
			store, condition := app.serviceStore()
			if condition != nil {
				t.Fatal(condition)
			}
			now := time.Now().UTC()
			if err := store.RestoreRecord(services.Record{ID: tasks.ID(), Profile: "app", Directory: privateTempDir(t), Command: "web", CreatedAt: now, EndedAt: &now, State: "stopped"}); err != nil {
				t.Fatal(err)
			}
			prepare, err := maintenance.AcquireExclusive(context.Background(), store.Data)
			if err != nil {
				t.Fatal(err)
			}
			prepare()
			release, err := maintenance.LockExclusive(context.Background(), filepath.Join(store.Data, ".retained-bodies.lock"))
			if err != nil {
				t.Fatal(err)
			}
			defer release()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			timeout := "650ms"
			if tc.cancel {
				timeout = "5s"
				timer := time.AfterFunc(650*time.Millisecond, cancel)
				defer timer.Stop()
			}
			var out, diagnostic bytes.Buffer
			code := app.Run(ctx, []string{"--format", tc.format, "doctor", "--storage", "--fix", "--timeout", timeout}, IO{Out: &out, Err: &diagnostic})
			if code == 0 || out.Len() != 0 {
				t.Fatalf("blocked upgrade succeeded: %d %q %q", code, out.String(), diagnostic.String())
			}
			text := diagnostic.String()
			hasFeedback := strings.Contains(text, "Waiting for retained data access")
			if hasFeedback != tc.feedback {
				t.Fatalf("progress policy changed: %q", text)
			}
			if tc.animate {
				lastClear := strings.LastIndex(text, "\r\x1b[2K")
				if lastClear < 0 || !strings.Contains(text[lastClear+len("\r\x1b[2K"):], "Error:") {
					t.Fatalf("progress was not cleared before the final diagnostic: %q", text)
				}
			} else if strings.Contains(text, "\r") || strings.Contains(text, "\x1b[2K") || (!tc.environment.Stderr.IsTerminal || tc.environment.DumbTerminal) && strings.Contains(text, "\x1b") {
				t.Fatalf("terminal controls reached a plain stream: %q", text)
			}
			if tc.format == "json" && !json.Valid(diagnostic.Bytes()) {
				t.Fatalf("progress corrupted the JSON envelope: %q", text)
			}
		})
	}
}
