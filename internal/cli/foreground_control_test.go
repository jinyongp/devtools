package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jinyongp/devtools/internal/services"
	"github.com/jinyongp/devtools/internal/tasks"
)

func TestForegroundExecutionUsesProcessInventoryAndStop(t *testing.T) {
	app := testApp(t)
	dir := privateTempDir(t)
	t.Chdir(dir)
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ready := filepath.Join(dir, "child-ready")
	args, _ := json.Marshal([]string{executable, "-test.run=^TestForegroundExecutionChild$", "--", ready})
	probe, _ := json.Marshal([]string{executable, "-test.run=^TestForegroundExecutionChild$", "--", "probe"})
	config := "profile='app'\n[ports.web]\n[commands.web]\nexec=" + string(args) + "\nserve=['web']\n[commands.web.ready]\nexec=" + string(probe) + "\n"
	if err := os.WriteFile("devtools.toml", []byte(config), 0600); err != nil {
		t.Fatal(err)
	}
	store, condition := app.serviceStore()
	if condition != nil {
		t.Fatal(condition)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	var stdout, stderr bytes.Buffer
	done := make(chan struct{})
	code := 0
	go func() {
		defer close(done)
		code = app.Run(ctx, []string{"run", "web"}, IO{Out: &stdout, Err: &stderr})
	}()
	defer func() { cancel(); <-done }()
	for {
		if _, err := os.Stat(ready); err == nil {
			break
		}
		select {
		case <-done:
			t.Fatalf("run exited before readiness: %d %s", code, stderr.String())
		case <-ctx.Done():
			t.Fatal("foreground command did not start")
		case <-time.After(10 * time.Millisecond):
		}
	}
	rows, condition := store.Navigation(ctx, services.NavigationSelection{Profile: "app", Directory: dir}, nil)
	if condition != nil || len(rows) != 1 || rows[0].ActiveCount != 1 || rows[0].State != "running" || rows[0].ExecutionID == "" {
		t.Fatalf("foreground inventory: %#v %v", rows, condition)
	}
	id := rows[0].ExecutionID
	status, condition := store.Status(ctx, id)
	if condition != nil || status.Mode != "foreground" || status.ID != id {
		t.Fatalf("foreground status: %#v %v", status, condition)
	}
	check, condition := store.Check(ctx, id)
	if condition != nil || check.Readiness == nil || !check.Readiness.Ready {
		t.Fatalf("foreground readiness: %#v %v", check, condition)
	}
	request := services.Request{Action: "stop", ID: id, RequestID: tasks.ID()}
	stopped, condition := store.Apply(ctx, request)
	if condition != nil || stopped.Item.State != "stopped" || !stopped.Changed {
		t.Fatalf("foreground stop: %#v %v", stopped, condition)
	}
	replay, condition := store.Apply(ctx, request)
	if condition != nil || !replay.Replayed || replay.Item.ID != id {
		t.Fatalf("foreground replay: %#v %v", replay, condition)
	}
	<-done
	if code == 0 || stdout.String() != "native child output\n" || stderr.Len() != 0 {
		t.Fatalf("native streams and exit: %d %q %q", code, stdout.String(), stderr.String())
	}
}

func TestForegroundExecutionChild(t *testing.T) {
	if len(os.Args) < 3 || os.Args[len(os.Args)-2] != "--" {
		return
	}
	path := os.Args[len(os.Args)-1]
	if path == "probe" {
		os.Exit(0)
	}
	if filepath.Base(path) != "child-ready" {
		return
	}
	if os.WriteFile(path, nil, 0600) != nil {
		os.Exit(91)
	}
	_, _ = os.Stdout.WriteString("native child output\n")
	time.Sleep(30 * time.Second)
	os.Exit(92)
}
