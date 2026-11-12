package maintenance

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/jinyongp/devtools/internal/platformfs"
)

func TestRuntimeTransitionResumesAfterMoveBeforeClosure(t *testing.T) {
	ctx := context.Background()
	root := privateTempDir(t)
	registry := filepath.Join(root, "ports", "registry.json")
	if err := Write(registry, []byte("original registry")); err != nil {
		t.Fatal(err)
	}

	withRuntimeGate(t, ctx, root, func() {
		handle, err := platformfs.OpenRoot(root, false)
		if err != nil {
			t.Fatal(err)
		}
		defer handle.Close()
		if err := handle.MkdirAll(VersionedRuntime); err != nil {
			t.Fatal(err)
		}
		if err := handle.WriteFile(ctx, runtimeTransitionFile, []byte(runtimeClosing), false); err != nil {
			t.Fatal(err)
		}
		if err := FenceLegacyRecoveryHeld(root); err != nil {
			t.Fatal(err)
		}
		if err := stageRuntimeDirectory(ctx, handle, root, "ports", filepath.Join(VersionedRuntime, "ports")); err != nil {
			t.Fatal(err)
		}
	})
	requireTransitionPending(t, ctx, root)
	if dir := RuntimeDirectory(root, "ports"); dir == filepath.Dir(registry) {
		t.Fatal("partially moved registry was presented as missing in the legacy location")
	}

	withRuntimeGate(t, ctx, root, func() {
		if err := StageRuntimeTransitionHeld(ctx, root); err != nil {
			t.Fatal(err)
		}
	})
	requireTransitionPending(t, ctx, root)
	writerActive := errors.New("writer still holds its original lease")
	withRuntimeGate(t, ctx, root, func() {
		err := PublishRuntimeTransitionHeld(ctx, root, func(context.Context, string) error { return writerActive })
		if !errors.Is(err, writerActive) {
			t.Fatalf("unexpected result with active writer: %v", err)
		}
	})
	requireTransitionPending(t, ctx, root)

	withRuntimeGate(t, ctx, root, func() {
		err := PublishRuntimeTransitionHeld(ctx, root, func(_ context.Context, staged string) error {
			if body, err := Read(filepath.Join(staged, "ports", "registry.json"), 1024); err != nil || string(body) != "original registry" {
				t.Fatal("prepared registry changed", err)
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	})
	release, err := AcquirePassiveQuery(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	body, err := Read(filepath.Join(RuntimeDirectory(root, "ports"), "registry.json"), 1024)
	release()
	if err != nil || string(body) != "original registry" {
		t.Fatal("original registry was not visible after verification", err)
	}
	legacy := filepath.Dir(registry)
	if err := os.Remove(legacy); err != nil {
		t.Fatal(err)
	}
	if err := platformfs.MkdirPrivate(legacy); err != nil {
		t.Fatal(err)
	}
	if RuntimeDirectory(root, "ports") == legacy {
		t.Fatal("published runtime fell back to a recreated legacy directory")
	}
}

func TestRuntimeTransitionPreservesRecreatedLegacyDirectory(t *testing.T) {
	ctx := context.Background()
	root := privateTempDir(t)
	withRuntimeGate(t, ctx, root, func() {
		if err := Write(filepath.Join(root, runtimeTransitionFile), []byte(runtimeClosing)); err != nil {
			t.Fatal(err)
		}
		legacy := filepath.Join(root, "ports", "registry.json")
		staged := filepath.Join(root, VersionedRuntime, "ports", "registry.json")
		for path, value := range map[string]string{legacy: "recreated", staged: "original"} {
			if err := Write(path, []byte(value)); err != nil {
				t.Fatal(err)
			}
		}
		if err := StageRuntimeTransitionHeld(ctx, root); err == nil {
			t.Fatal("recreated legacy state was accepted")
		}
		for path, value := range map[string]string{legacy: "recreated", staged: "original"} {
			if body, err := Read(path, 1024); err != nil || string(body) != value {
				t.Fatal("conflict destroyed one of the registries", err)
			}
		}
	})
	requireTransitionPending(t, ctx, root)
}

func withRuntimeGate(t *testing.T, ctx context.Context, root string, fn func()) {
	t.Helper()
	release, err := AcquireForRecovery(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	fn()
}

func requireTransitionPending(t *testing.T, ctx context.Context, root string) {
	t.Helper()
	for _, acquire := range []func(context.Context, string) (func(), error){AcquirePassiveQuery, AcquireShared, AcquireExclusive} {
		release, err := acquire(ctx, root)
		if release != nil {
			release()
		}
		if !errors.Is(err, ErrRuntimeTransitionPending) {
			t.Fatalf("pending runtime admitted a reader or writer: %v", err)
		}
	}
	for _, domain := range runtimeDomains {
		if _, err := os.Stat(RuntimeDirectory(root, domain)); err == nil {
			t.Fatal("pending runtime exposed a domain", domain)
		}
	}
}
