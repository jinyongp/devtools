//go:build !windows

package cli

import (
	"encoding/json"
	"os"
	"testing"
)

func TestForegroundOneOffStillRunsWithReadOnlyTrackingStorage(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("requires native permission enforcement")
	}
	app := testApp(t)
	dir := privateTempDir(t)
	t.Chdir(dir)
	args, err := json.Marshal(runBoundaryCommand(t))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile("devtools.toml", []byte("profile='app'\n[commands.plain]\nexec="+string(args)+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	store, condition := app.serviceStore()
	if condition != nil {
		t.Fatal(condition)
	}
	if err := os.Chmod(store.Data, 0500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(store.Data, 0700) })
	t.Setenv("LEVEL", "original")
	code, stdout, stderr := invoke(t, app, "", "run", "plain", "value")
	if code != 0 || stdout != "original|<value>" || stderr != "" {
		t.Fatalf("read-only foreground execution regressed: %d %q %q", code, stdout, stderr)
	}
}
