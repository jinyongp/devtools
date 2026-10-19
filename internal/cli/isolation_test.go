package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/jinyongp/devtools/internal/process"
	"github.com/jinyongp/devtools/internal/protocol"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestIsolationFlagPrecedesEnvironmentAndDoesNotCreateReadPaths(t *testing.T) {
	parent, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	fromEnv := filepath.Join(parent, "env")
	fromFlag := filepath.Join(parent, "flag")
	t.Setenv("DEVTOOLS_ISOLATED_ROOT", fromEnv)
	a := New("test", "test")
	var output, errors bytes.Buffer
	code := a.Run(context.Background(), []string{"project", "inspect", "--profile", "app", "--isolated-root", fromFlag}, IO{Out: &output, Err: &errors})
	if code != 0 {
		t.Fatal(code, errors.String())
	}
	var envelope struct {
		Data struct {
			Paths struct{ Config, Data, Cache, Runtime string }
		}
	}
	if e := json.Unmarshal(output.Bytes(), &envelope); e != nil {
		t.Fatal(e)
	}
	p := envelope.Data.Paths
	expectedRoot := fromFlag
	if p.Data != filepath.Join(expectedRoot, "data") || p.Config != filepath.Join(expectedRoot, "config") || p.Cache != filepath.Join(expectedRoot, "cache") || p.Runtime != filepath.Join(expectedRoot, "runtime") {
		t.Fatal(p)
	}
	for _, root := range []string{fromFlag, fromEnv} {
		if _, e := os.Stat(root); !os.IsNotExist(e) {
			t.Fatal("read created root", root, e)
		}
	}
	output.Reset()
	errors.Reset()
	if a.Run(context.Background(), []string{"--isolated-root", fromFlag, "project", "inspect", "--profile", "app"}, IO{Out: &output, Err: &errors}) != 0 {
		t.Fatal(errors.String())
	}
	if os.Getenv("DEVTOOLS_ISOLATED_ROOT") != fromEnv {
		t.Fatal("flag changed global environment")
	}
}

func TestIsolationOptionAfterConfiguredNameRemainsChildArgument(t *testing.T) {
	c := Command{Name: "command run", Options: []Option{{Name: "isolated-root"}}, Arguments: []Argument{{Name: "command"}}, ChildArgs: true}
	r, e := parseRequest(c, []string{"web", "--isolated-root", "child-value"})
	if e != nil || len(r.Child) != 2 || r.Child[0] != "--isolated-root" || r.Options["isolated-root"] != "" {
		t.Fatal(r, e)
	}
}

func TestRunFreezesExplicitIsolationBeforeChildInput(t *testing.T) {
	root := privateTempDir(t)
	selected := filepath.Join(root, "selected")
	t.Setenv("DEVTOOLS_ISOLATED_ROOT", filepath.Join(root, "inherited"))
	exe, _ := os.Executable()
	app := New("test", "test")
	observed := ""
	ctx := process.WithRunner(context.Background(), func(_ context.Context, _ []string, _ string, env []string, _ io.Reader, _ io.Writer, _ io.Writer) (int, *protocol.Error) {
		for _, item := range env {
			if strings.HasPrefix(item, "DEVTOOLS_ISOLATED_ROOT=") {
				observed = strings.TrimPrefix(item, "DEVTOOLS_ISOLATED_ROOT=")
			}
		}
		return 0, nil
	})
	var out, diagnostic bytes.Buffer
	code := app.Run(ctx, []string{"run", "--profile", "isolation-fixture", "--isolated-root", selected, "--", exe}, IO{Out: &out, Err: &diagnostic})
	if code != 0 || observed != selected {
		t.Fatalf("frozen input lost explicit isolation: code=%d observed=%q error=%s", code, observed, diagnostic.String())
	}
}
