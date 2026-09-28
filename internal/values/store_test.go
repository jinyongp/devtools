package values

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/jinyongp/devtools/internal/maintenance"
	"github.com/jinyongp/devtools/internal/protocol"
)

func TestLayeringAndKinds(t *testing.T) {
	s := newState("app")
	if _, err := s.Set(Variable, "LEVEL", "missing", "x"); err == nil || err.Code != "env_not_found" {
		t.Fatal(err)
	}
	s.CreateEnv("local")
	s.Set(Variable, "LEVEL", "", "info")
	s.Set(Variable, "LEVEL", "local", "debug")
	s.Set(Secret, "TOKEN", "", "sensitive")
	value, meta, err := s.GetVariable("LEVEL", "local")
	if err != nil || value != "debug" || !meta.Overrides || meta.Source != "env" {
		t.Fatalf("%s %+v %v", value, meta, err)
	}
	if _, _, err := s.GetVariable("TOKEN", ""); err == nil || err.Code != "kind_conflict" {
		t.Fatal(err)
	}
	if _, err := s.Set(Variable, "TOKEN", "local", "public"); err == nil || err.Code != "kind_conflict" {
		t.Fatal(err)
	}
	if _, err := s.RemoveEnv("local"); err == nil || err.Code != "env_not_empty" {
		t.Fatal(err)
	}
	s.Unset(Variable, "LEVEL", "local")
	value, _, err = s.GetVariable("LEVEL", "local")
	if err != nil || value != "info" {
		t.Fatalf("fallback %q %v", value, err)
	}
	s.Set(Variable, "LEVEL", "local", "")
	value, _, err = s.GetVariable("LEVEL", "local")
	if err != nil || value != "" {
		t.Fatalf("empty override %q %v", value, err)
	}
	s.Unset(Variable, "LEVEL", "local")
	if _, err := s.RemoveEnv("local"); err != nil {
		t.Fatal(err)
	}
	s.Unset(Secret, "TOKEN", "")
	if _, err := s.Set(Variable, "TOKEN", "", "public"); err == nil {
		t.Fatal("lost kind after unset")
	}
	for _, value := range []string{"a\x00b", string([]byte{255})} {
		if _, err := s.Set(Secret, "BAD", "", value); err == nil {
			t.Fatal("accepted invalid value")
		}
	}
}

func TestStoreConcurrentUpdates(t *testing.T) {
	store := Store{Directory: filepath.Join(t.TempDir(), "profiles"), Profile: "App"}
	var group sync.WaitGroup
	for i := range 20 {
		group.Go(func() {
			_, err := store.Update(context.Background(), func(s *State) (bool, *protocol.Error) { return s.Set(Variable, fmt.Sprintf("KEY_%d", i), "", "value") })
			if err != nil {
				t.Error(err)
			}
		})
	}
	group.Wait()
	state, err := store.Read(context.Background())
	if err != nil || len(state.Keys) != 20 {
		t.Fatalf("lost updates: %+v %v", state, err)
	}
	for _, path := range []string{store.Directory, store.file(), store.file() + ".lock"} {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		want := os.FileMode(0600)
		if info.IsDir() {
			want = 0700
		}
		if info.Mode().Perm() != want {
			t.Fatalf("%s: mode %v", path, info.Mode())
		}
	}
	other := Store{Directory: store.Directory, Profile: "app"}
	otherState, err := other.Read(context.Background())
	if err != nil || len(otherState.Keys) != 0 {
		t.Fatal("case-sensitive profiles collided")
	}
	files, _ := os.ReadDir(store.Directory)
	for _, file := range files {
		if strings.HasPrefix(file.Name(), ".update-") {
			t.Fatal("temporary snapshot left")
		}
	}
}

func TestStorageCorruptionAndPermissions(t *testing.T) {
	store := Store{Directory: filepath.Join(t.TempDir(), "profiles"), Profile: "app"}
	_, err := store.Update(context.Background(), func(s *State) (bool, *protocol.Error) { return s.Set(Secret, "TOKEN", "", "CANARY") })
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(store.file(), []byte("CANARY invalid json"), 0600); err != nil {
		t.Fatal(err)
	}
	_, err = store.Update(context.Background(), func(s *State) (bool, *protocol.Error) { return s.CreateEnv("local") })
	if err == nil || err.Code != "invalid_storage" || strings.Contains(err.Error(), "CANARY") {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(store.file())
	if string(data) != "CANARY invalid json" {
		t.Fatal("overwrote corrupt storage")
	}
	if err := os.Chmod(store.file(), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Read(context.Background()); err == nil || err.Code != "storage_error" {
		t.Fatal(err)
	}
	if err := os.Remove(store.file()); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(t.TempDir(), "target")
	if err := os.WriteFile(target, []byte("original"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, store.file()); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Read(context.Background()); err == nil {
		t.Fatal("followed storage symlink")
	}
}

func TestLockCancellation(t *testing.T) {
	store := Store{Directory: filepath.Join(t.TempDir(), "profiles"), Profile: "app"}
	if err := os.Mkdir(store.Directory, 0700); err != nil {
		t.Fatal(err)
	}
	lock, err := os.OpenFile(store.file()+".lock", os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX); err != nil {
		t.Fatal(err)
	}
	defer syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	called := false
	_, apiErr := store.Update(ctx, func(s *State) (bool, *protocol.Error) { called = true; return s.CreateEnv("local") })
	if apiErr == nil || apiErr.Code != "canceled" || called {
		t.Fatalf("%v called=%v", apiErr, called)
	}
}

func TestMaintenanceGateCancellation(t *testing.T) {
	root := t.TempDir()
	store := Store{Directory: filepath.Join(root, "profiles"), Profile: "app"}
	unlock, err := maintenance.Acquire(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()

	check := func(name string, call func(context.Context) *protocol.Error) {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
		defer cancel()
		started := time.Now()
		apiErr := call(ctx)
		if apiErr == nil || apiErr.Code != "canceled" || apiErr.ExitCode != 130 {
			t.Fatalf("%s: expected canceled, got %v", name, apiErr)
		}
		if elapsed := time.Since(started); elapsed > 500*time.Millisecond {
			t.Fatalf("%s: cancellation waited too long: %v", name, elapsed)
		}
	}
	check("read", func(ctx context.Context) *protocol.Error {
		_, apiErr := store.Read(ctx)
		return apiErr
	})
	check("update", func(ctx context.Context) *protocol.Error {
		_, apiErr := store.Update(ctx, func(state *State) (bool, *protocol.Error) {
			return state.CreateEnv("local")
		})
		return apiErr
	})
	check("inspect", func(ctx context.Context) *protocol.Error {
		_, apiErr := store.Inspect(ctx, "")
		return apiErr
	})
}
