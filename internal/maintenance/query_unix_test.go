//go:build linux || darwin

package maintenance

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

func queryFixture(t *testing.T) string {
	t.Helper()
	root := privateTempDir(t)
	release, err := AcquireShared(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	release()
	return root
}

func TestQueryInitializesMissingLocks(t *testing.T) {
	for _, missing := range []string{"root", "directory", "turnstile", "gate"} {
		t.Run(missing, func(t *testing.T) {
			root := queryFixture(t)
			switch missing {
			case "root":
				root = filepath.Join(root, "new")
			case "directory":
				if err := os.RemoveAll(filepath.Join(root, ".maintenance")); err != nil {
					t.Fatal(err)
				}
			default:
				name := "lock"
				if missing == "turnstile" {
					name += ".turnstile"
				}
				if err := os.Remove(filepath.Join(root, ".maintenance", name)); err != nil {
					t.Fatal(err)
				}
			}
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			release, err := AcquireQuery(ctx, root)
			if err != nil {
				t.Fatal("initialization failed or retained its first lock", err)
			}
			release()
			for _, name := range []string{"lock", "lock.turnstile"} {
				if info, err := os.Stat(filepath.Join(root, ".maintenance", name)); err != nil || info.Mode().Perm() != 0600 {
					t.Fatal("missing private lock", err)
				}
			}
		})
	}
}

func TestQueryRecoveryPreservesTransactionOutcome(t *testing.T) {
	for _, protected := range []bool{false, true} {
		for _, phase := range []string{"prepared", "partial", "committed", "ready"} {
			t.Run(phase+"/protected="+map[bool]string{false: "false", true: "true"}[protected], func(t *testing.T) {
				root := queryFixture(t)
				release, err := AcquireExclusive(context.Background(), root)
				if err != nil {
					t.Fatal(err)
				}
				vaultID := strings.Repeat("a", 32)
				if protected {
					if err := writeProtectedRoot(root, vaultID); err != nil {
						t.Fatal(err)
					}
				}
				path := filepath.Join(root, "profiles", "61.json")
				if err := Write(path, []byte("before")); err != nil {
					t.Fatal(err)
				}
				if phase != "ready" {
					files := map[string][]byte{"profiles/61.json": []byte("after"), "tasks/61.json": []byte("new")}
					pointer, err := prepareTransaction(root, files)
					if err != nil {
						t.Fatal(err)
					}
					if phase != "prepared" {
						if err := applyReplacement(root, files); err != nil {
							t.Fatal(err)
						}
					}
					if phase == "committed" {
						if ok, err := markCommitted(root, pointer); err != nil || !ok {
							t.Fatal(err)
						}
					}
				}
				release()
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				release, err = AcquireQuery(ctx, root)
				if err != nil {
					t.Fatal(err)
				}
				defer release()
				want := "before"
				if phase == "committed" {
					want = "after"
				}
				if got := mustRead(t, path); got != want {
					t.Fatalf("recovery = %q, want %q", got, want)
				}
				if needed, err := recoveryNeeded(root); err != nil || needed {
					t.Fatal("query admitted pending recovery", err)
				}
				if _, got, err := ProtectedRootHeld(root); err != nil || got != protected {
					t.Fatal("protection boundary changed", err)
				}
			})
		}
	}
}

func TestQueryRecoversLegacyAndOrphans(t *testing.T) {
	for _, legacy := range []bool{false, true} {
		t.Run(map[bool]string{false: "orphan", true: "legacy"}[legacy], func(t *testing.T) {
			root := queryFixture(t)
			if legacy {
				body, _ := json.Marshal([]before{{Path: "profiles/61.json", Data: []byte("before"), Exists: true}})
				if err := Write(pendingPath(root), body); err != nil {
					t.Fatal(err)
				}
				if err := Write(filepath.Join(root, "profiles", "61.json"), []byte("partial")); err != nil {
					t.Fatal(err)
				}
			} else {
				id, _ := transactionID()
				if _, _, err := createTransactionDirectories(root, id); err != nil {
					t.Fatal(err)
				}
			}
			release, err := AcquireQuery(context.Background(), root)
			if err != nil {
				t.Fatal(err)
			}
			defer release()
			if needed, err := recoveryNeeded(root); err != nil || needed {
				t.Fatal("recovery not finished", err)
			}
			if legacy && mustRead(t, filepath.Join(root, "profiles", "61.json")) != "before" {
				t.Fatal("legacy partial snapshot admitted")
			}
		})
	}
}

func TestQueryRejectsUnsafeLockWithoutFallback(t *testing.T) {
	for _, kind := range []string{"directory_permissions", "file_permissions", "symlink", "directory", "fifo", "bad_gate", "bad_pointer"} {
		t.Run(kind, func(t *testing.T) {
			root := queryFixture(t)
			path := filepath.Join(root, ".maintenance", "lock.turnstile")
			switch kind {
			case "directory_permissions":
				if err := os.Chmod(filepath.Dir(path), 0755); err != nil {
					t.Fatal(err)
				}
			case "file_permissions":
				if err := os.Chmod(path, 0644); err != nil {
					t.Fatal(err)
				}
			case "symlink", "directory", "fifo", "bad_gate":
				if kind == "bad_gate" {
					path = filepath.Join(root, ".maintenance", "lock")
				}
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
				var err error
				switch kind {
				case "symlink":
					err = os.Symlink(filepath.Join(root, "absent"), path)
				case "fifo":
					err = syscall.Mkfifo(path, 0600)
				default:
					err = os.Mkdir(path, 0700)
				}
				if err != nil {
					t.Fatal(err)
				}
			case "bad_pointer":
				if err := Write(pendingPath(root), []byte("invalid")); err != nil {
					t.Fatal(err)
				}
			}
			if release, err := AcquireQuery(context.Background(), root); err == nil {
				release()
				t.Fatal("unsafe storage accepted")
			} else {
				var preparation *QueryPreparationError
				if errors.As(err, &preparation) {
					t.Fatal("unsafe storage sent to writable fallback")
				}
			}
			if kind == "bad_gate" || kind == "bad_pointer" {
				turn, acquired, err := tryAcquireFileLock(context.Background(), filepath.Join(root, ".maintenance", "lock.turnstile"), lockExclusive)
				if err != nil || !acquired {
					t.Fatal("failed query retained turnstile", err)
				}
				unlockFile(turn)
			}
		})
	}
}

func TestQuerySharesGateAndHonorsWriterIntent(t *testing.T) {
	root := queryFixture(t)
	first, err := AcquireQuery(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if first != nil {
			first()
		}
	}()
	second, err := AcquireQuery(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	second()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	writerDone := make(chan error, 1)
	go func() {
		release, err := AcquireExclusive(ctx, root)
		if err == nil {
			release()
		}
		writerDone <- err
	}()
	waitForWriterIntent(t, filepath.Join(root, ".maintenance", "lock.turnstile"))
	deadline, stop := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer stop()
	if release, err := AcquireQuery(deadline, root); !errors.Is(err, context.DeadlineExceeded) {
		if release != nil {
			release()
		}
		t.Fatal("query bypassed waiting writer", err)
	}
	first()
	first = nil
	if err := <-writerDone; err != nil {
		t.Fatal(err)
	}
	release, err := AcquireQuery(ctx, root)
	if err != nil {
		t.Fatal("canceled reader retained lock", err)
	}
	release()
}

func TestQueryGateWaitCancellationReleasesTurnstile(t *testing.T) {
	root := queryFixture(t)
	directory := filepath.Join(root, ".maintenance")
	gate, err := acquireFileLock(context.Background(), filepath.Join(directory, "lock"), lockExclusive)
	if err != nil {
		t.Fatal(err)
	}
	defer unlockFile(gate)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	if _, err := AcquireQuery(ctx, root); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
	turn, acquired, err := tryAcquireFileLock(context.Background(), filepath.Join(directory, "lock.turnstile"), lockExclusive)
	if err != nil || !acquired {
		t.Fatal("query canceled during second lock retained turnstile", err)
	}
	unlockFile(turn)
}

func TestQueryCancellationAndFallbackFailure(t *testing.T) {
	root := queryFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := AcquireQuery(ctx, root); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	// Initialization cannot create children below an ordinary file. The missing
	// maintenance path must fail with its real OS cause, rather than empty data.
	file := filepath.Join(root, "not-a-directory")
	if err := os.WriteFile(file, nil, 0600); err != nil {
		t.Fatal(err)
	}
	_, err := acquireQueryFallback(context.Background(), file, "initialization_required")
	var preparation *QueryPreparationError
	if !errors.As(err, &preparation) || preparation.State != "initialization_required" || preparation.Unwrap() == nil {
		t.Fatal("fallback cause or state missing", err)
	}
	if strings.Contains(err.Error(), root) {
		t.Fatal("private path escaped through preparation error")
	}
}
