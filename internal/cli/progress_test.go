package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jinyongp/devtools/internal/cli/presentation"
	"github.com/jinyongp/devtools/internal/maintenance"
	"github.com/jinyongp/devtools/internal/process"
	"github.com/jinyongp/devtools/internal/protocol"
	"github.com/jinyongp/devtools/internal/services"
	"github.com/jinyongp/devtools/internal/tasks"
)

func TestProgressCoversHandlersWithoutDomainEvents(t *testing.T) {
	terminal := presentation.Terminal{IsTerminal: true, ANSI: true, Width: 120}
	for _, tc := range []struct {
		name, format, command string
		environment           presentation.Environment
		feedback              bool
		omitStderr            bool
	}{
		{"terminal", "text", "version", presentation.Environment{Stdout: terminal, Stderr: terminal}, true, false},
		{"pipe", "text", "version", presentation.Environment{}, true, false},
		{"json", "json", "version", presentation.Environment{Stdout: terminal, Stderr: terminal}, false, false},
		{"agent", "text", "version", presentation.Environment{Stdout: terminal, Stderr: terminal, Signals: []string{"AI_AGENT"}}, false, false},
		{"artifact", "text", "license", presentation.Environment{Stdout: terminal, Stderr: terminal}, false, false},
		{"missing_stderr", "text", "version", presentation.Environment{}, false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			app := presentationApp(tc.environment)
			for i := range app.commands {
				if app.commands[i].Name == tc.command {
					run := app.commands[i].Run
					app.commands[i].Run = func(ctx context.Context, streams IO, r Request) (any, *protocol.Error) {
						time.Sleep(450 * time.Millisecond)
						return run(ctx, streams, r)
					}
				}
			}
			var output, errors bytes.Buffer
			streams := IO{Out: &output, Err: &errors}
			if tc.omitStderr {
				streams.Err = nil
			}
			code := app.Run(context.Background(), []string{"--format", tc.format, tc.command}, streams)
			out, diagnostic := output.String(), errors.String()
			if code != 0 || out == "" || strings.Contains(diagnostic, "Running devtools "+tc.command) != tc.feedback {
				t.Fatalf("missing common progress or changed result: %d %q %q", code, out, diagnostic)
			}
			if !tc.feedback && diagnostic != "" || tc.format == "json" && !json.Valid([]byte(out)) {
				t.Fatalf("native/machine output was contaminated: %q %q", out, diagnostic)
			}
		})
	}
}

func TestForegroundOutputOwnsTheTerminalDuringExecution(t *testing.T) {
	app := testApp(t)
	terminal := presentation.Terminal{IsTerminal: true, ANSI: true, Width: 120}
	app.observeOutput = func(IO) presentation.Environment {
		return presentation.Environment{Stdout: terminal, Stderr: terminal}
	}
	t.Chdir(privateTempDir(t))
	if err := os.WriteFile("devtools.toml", []byte("profile='app'\n[commands.work]\nexec="+fixtureExec(t, "true")+"\ninject=false\n"), 0600); err != nil {
		t.Fatal(err)
	}
	ctx := process.WithRunner(context.Background(), func(_ context.Context, _ []string, _ string, _ []string, _ io.Reader, out, diagnostic io.Writer) (int, *protocol.Error) {
		_, _ = io.WriteString(out, "native output\n")
		_, _ = io.WriteString(diagnostic, "native diagnostic\n")
		time.Sleep(650 * time.Millisecond)
		return 17, nil
	})
	var out, diagnostic bytes.Buffer
	code := app.Run(ctx, []string{"--format", "text", "run", "work"}, IO{Out: &out, Err: &diagnostic})
	if code != 17 || out.String() != "native output\n" || diagnostic.String() != "native diagnostic\n" {
		t.Fatalf("progress interrupted the child: %d %q %q", code, out.String(), diagnostic.String())
	}
}

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
