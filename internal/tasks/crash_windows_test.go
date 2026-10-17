package tasks

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

func TestWindowsCrashWriter(t *testing.T) {
	stage := os.Getenv("DEVTOOLS_STORAGE_CRASH_STAGE")
	if stage == "" {
		return
	}
	store := Store{Directory: os.Getenv("DEVTOOLS_STORAGE_CRASH_DIRECTORY"), Profile: "native-crash"}
	store.commitV3 = func(commit func() error) error {
		if stage == "after-wal" {
			if err := commit(); err != nil {
				return err
			}
		}
		// This is a real abrupt process termination: no Go defers, lock Close,
		// scratch cleanup or response publication gets a chance to run.
		_ = windows.TerminateProcess(windows.CurrentProcess(), 71)
		panic("TerminateProcess returned")
	}
	_, err := store.Execute(context.Background(), Request{Action: "task.add", Body: Object{"title": "after-crash"}, Options: map[string]string{"request-id": os.Getenv("DEVTOOLS_STORAGE_CRASH_REQUEST")}})
	if err != nil {
		t.Fatal(err)
	}
	t.Fatal("crash hook was not reached")
}
func TestWindowsAbruptWALRecoveryAndRequestReplay(t *testing.T) {
	for _, stage := range []string{"before-wal", "after-wal"} {
		t.Run(stage, func(t *testing.T) {
			store := Store{Directory: filepath.Join(privateTempDir(t), "tasks"), Profile: "native-crash"}
			call(t, store, "task.add", "", Object{"title": "seed"})
			requestID := ID()
			binary, err := os.Executable()
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			child := exec.CommandContext(ctx, binary, "-test.run=^TestWindowsCrashWriter$")
			child.Env = append(os.Environ(), "DEVTOOLS_STORAGE_CRASH_STAGE="+stage, "DEVTOOLS_STORAGE_CRASH_DIRECTORY="+store.Directory, "DEVTOOLS_STORAGE_CRASH_REQUEST="+requestID)
			err = child.Run()
			var exit *exec.ExitError
			if !errors.As(err, &exit) || exit.ExitCode() != 71 {
				t.Fatal("writer did not reach crash boundary", err)
			}
			request := Request{Action: "task.add", Body: Object{"title": "after-crash"}, Options: map[string]string{"request-id": requestID}}
			first, e := store.Execute(context.Background(), request)
			if e != nil {
				t.Fatal("recovery/retry failed", e)
			}
			if (first["replayed"] == true) != (stage == "after-wal") {
				t.Fatal("WAL commit boundary replay mismatch")
			}
			again, e := store.Execute(context.Background(), request)
			if e != nil || again["replayed"] != true {
				t.Fatal("same request was not replayed", e)
			}
			state, e := store.Read(context.Background())
			if e != nil || len(state.Items) != 2 {
				t.Fatal("lost or duplicated mutation", e)
			}
			resolution, ok, err := resolveV3(store.Directory, store.Profile)
			if err != nil || !ok {
				t.Fatal("missing v3 head", err)
			}
			if _, exists, err := readPendingManifest(resolution); err != nil || exists {
				t.Fatal("pending not finalized", err)
			}
		})
	}
}
