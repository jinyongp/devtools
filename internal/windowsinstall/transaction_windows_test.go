package windowsinstall

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jinyongp/devtools/internal/platformfs"
	"golang.org/x/sys/windows"
)

func fixtureEngine(t *testing.T, directory string) *engine {
	t.Helper()
	root, err := platformfs.OpenRoot(directory, true)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { root.Close() })
	return &engine{root: root, directory: directory, smoke: func(context.Context, string, identity) error { return nil }}
}
func fixtureInstallation(t *testing.T, e *engine, alias bool) *manifest {
	t.Helper()
	body := []byte("original image")
	if err := e.root.WriteFile(context.Background(), "devtools.exe", body, false); err != nil {
		t.Fatal(err)
	}
	main, _, err := e.inspect("devtools.exe")
	if err != nil {
		t.Fatal(err)
	}
	m := &manifest{Format: 1, Identity: identity{"0.0.0-test.1", "fixture"}, Main: main}
	if alias {
		if err := e.root.WriteFile(context.Background(), "dvt.exe", body, false); err != nil {
			t.Fatal(err)
		}
		p, _, err := e.inspect("dvt.exe")
		if err != nil {
			t.Fatal(err)
		}
		m.Alias = &p
	}
	if err := e.writeJSON(context.Background(), manifestName, m); err != nil {
		t.Fatal(err)
	}
	return m
}
func assertOriginal(t *testing.T, e *engine, want *manifest) {
	t.Helper()
	if err := e.require("devtools.exe", want.Main); err != nil {
		t.Fatal(err)
	}
	if want.Alias != nil {
		if err := e.require("dvt.exe", *want.Alias); err != nil {
			t.Fatal(err)
		}
	}
	var got manifest
	if exists, err := e.readJSON(manifestName, &got); err != nil || !exists || !bytes.Equal(encode(got), encode(want)) {
		t.Fatalf("manifest changed during failed transaction: %+v %v", got, err)
	}
}
func TestWindowsInstallFailureRestoresBothImagesAndManifest(t *testing.T) {
	for _, phase := range []string{"prepared", "staged_main", "staged_alias", "old_main", "published_main", "old_alias", "published_alias", "manifest"} {
		t.Run(phase, func(t *testing.T) {
			e := fixtureEngine(t, filepath.Join(t.TempDir(), "bin"))
			before := fixtureInstallation(t, e, true)
			e.after = func(at string) error {
				if at == phase {
					return errors.New("injected filesystem/publication failure")
				}
				return nil
			}
			result, err := e.transact(context.Background(), Options{Action: "update"}, []byte("replacement image"), identity{"0.0.0-test.2", "fixture"}, before)
			if err == nil || result != nil {
				t.Fatalf("failed installation succeeded: %+v %v", result, err)
			}
			assertOriginal(t, e, before)
			var pending journal
			if exists, err := e.readJSON(journalName, &pending); exists || err != nil {
				t.Fatalf("ordinary rollback left incomplete journal: %v %v", exists, err)
			}
		})
	}
}
func TestWindowsInstallCancellationRestoresWithFreshContext(t *testing.T) {
	e := fixtureEngine(t, filepath.Join(t.TempDir(), "bin"))
	before := fixtureInstallation(t, e, true)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	e.after = func(phase string) error {
		if phase == "published_main" {
			cancel()
			return ctx.Err()
		}
		return nil
	}
	if _, err := e.transact(ctx, Options{Action: "update"}, []byte("replacement image"), identity{"0.0.0-test.2", "fixture"}, before); err == nil {
		t.Fatal("cancellation succeeded")
	}
	assertOriginal(t, e, before)
}
func TestWindowsInstallCrashChild(t *testing.T) {
	if os.Getenv("DEVTOOLS_INSTALL_CRASH_CHILD") != "1" {
		return
	}
	root, err := platformfs.OpenRoot(os.Getenv("DEVTOOLS_INSTALL_CRASH_DIR"), false)
	if err != nil {
		os.Exit(91)
	}
	lock, err := root.OpenExclusive(".devtools-install.lock", true, false)
	if err != nil {
		os.Exit(92)
	}
	defer lock.Close()
	e := &engine{root: root, directory: os.Getenv("DEVTOOLS_INSTALL_CRASH_DIR"), smoke: func(context.Context, string, identity) error { return nil }}
	var before manifest
	if exists, err := e.readJSON(manifestName, &before); !exists || err != nil {
		os.Exit(93)
	}
	e.after = func(phase string) error {
		if phase == os.Getenv("DEVTOOLS_INSTALL_CRASH_PHASE") {
			os.Exit(88)
		}
		return nil
	}
	e.transact(context.Background(), Options{Action: "update"}, []byte("replacement image"), identity{"0.0.0-test.2", "fixture"}, &before)
	os.Exit(94)
}
func TestWindowsInstallAbruptExitRecoversObservedNames(t *testing.T) {
	for _, phase := range []string{"staged_main", "old_main", "published_main", "old_alias", "published_alias", "manifest", "committed"} {
		t.Run(phase, func(t *testing.T) {
			directory := filepath.Join(t.TempDir(), "bin")
			e := fixtureEngine(t, directory)
			before := fixtureInstallation(t, e, true)
			exe, _ := os.Executable()
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, exe, "-test.run=^TestWindowsInstallCrashChild$")
			cmd.Env = append(os.Environ(), "DEVTOOLS_INSTALL_CRASH_CHILD=1", "DEVTOOLS_INSTALL_CRASH_DIR="+directory, "DEVTOOLS_INSTALL_CRASH_PHASE="+phase)
			if err := cmd.Run(); err == nil || cmd.ProcessState.ExitCode() != 88 {
				t.Fatalf("crash boundary not reached: %v", err)
			}
			lock, err := e.root.OpenExclusive(".devtools-install.lock", true, false)
			if err != nil {
				t.Fatal("crashed process retained lock", err)
			}
			defer lock.Close()
			if err = e.recover(context.Background()); err != nil {
				t.Fatal(err)
			}
			if phase != "committed" {
				assertOriginal(t, e, before)
			} else {
				for _, name := range []string{"devtools.exe", "dvt.exe"} {
					body, err := e.root.ReadFile(name, 1024)
					if err != nil || string(body) != "replacement image" {
						t.Fatalf("committed generation rolled back: %q %v", body, err)
					}
				}
			}
		})
	}
}
func TestWindowsInstallForeignAliasAndChangedOwnedFiles(t *testing.T) {
	t.Run("foreign alias", func(t *testing.T) {
		e := fixtureEngine(t, filepath.Join(t.TempDir(), "bin"))
		if err := e.root.WriteFile(context.Background(), "dvt.exe", []byte("foreign alias"), false); err != nil {
			t.Fatal(err)
		}
		result, err := e.transact(context.Background(), Options{Action: "install"}, []byte("replacement image"), identity{"0.0.0-test.2", "fixture"}, nil)
		if err != nil || result.Alias.Status != "skipped_conflict" {
			t.Fatalf("foreign alias: %+v %v", result, err)
		}
		body, err := e.root.ReadFile("dvt.exe", 1024)
		if err != nil || string(body) != "foreign alias" {
			t.Fatal("foreign alias replaced")
		}
	})
	t.Run("modified owned alias", func(t *testing.T) {
		directory := filepath.Join(t.TempDir(), "bin")
		e := fixtureEngine(t, directory)
		fixtureInstallation(t, e, true)
		if err := e.root.WriteFile(context.Background(), "dvt.exe", []byte("foreign replacement"), true); err != nil {
			t.Fatal(err)
		}
		_, err := apply(context.Background(), Options{Action: "update", BinDir: directory}, filepath.Join(directory, "devtools.exe"), func(context.Context, string) ([]byte, string, error) {
			t.Fatal("download started before ownership validation")
			return nil, "", nil
		})
		if err == nil {
			t.Fatal("changed alias accepted")
		}
		body, _ := e.root.ReadFile("dvt.exe", 1024)
		if string(body) != "foreign replacement" {
			t.Fatal("changed alias destroyed")
		}
	})
}
func TestWindowsInstallCorruptJournalPreservesArtifacts(t *testing.T) {
	e := fixtureEngine(t, filepath.Join(t.TempDir(), "bin"))
	before := fixtureInstallation(t, e, true)
	for _, body := range []string{`{"format":1,"unknown":true}`, `{"format":1,"id":"../../outside","phase":"prepared"}`} {
		if err := e.root.WriteFile(context.Background(), journalName, []byte(body), true); err != nil {
			t.Fatal(err)
		}
		if err := e.recover(context.Background()); err == nil {
			t.Fatal("corrupt journal accepted")
		}
		assertOriginal(t, e, before)
	}
}
func TestWindowsInstallLockInteroperatesWithDotNetFileShareNone(t *testing.T) {
	directory := filepath.Join(t.TempDir(), "bin")
	e := fixtureEngine(t, directory)
	lock, err := e.root.OpenExclusive(".devtools-install.lock", true, false)
	if err != nil {
		t.Fatal(err)
	}
	system, err := windows.GetSystemDirectory()
	if err != nil {
		t.Fatal(err)
	}
	powershell := filepath.Join(system, "WindowsPowerShell", "v1.0", "powershell.exe")
	command := `try { $f=[IO.File]::Open($env:DEVTOOLS_INSTALL_TEST_LOCK,[IO.FileMode]::Open,[IO.FileAccess]::ReadWrite,[IO.FileShare]::None);$f.Dispose();Write-Output 'acquired' } catch [IO.IOException] { Write-Output 'busy' }`
	invoke := func() string {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, powershell, "-NoProfile", "-NonInteractive", "-Command", command)
		cmd.Env = append(os.Environ(), "DEVTOOLS_INSTALL_TEST_LOCK="+filepath.Join(directory, ".devtools-install.lock"))
		output, err := cmd.Output()
		if err != nil {
			t.Fatal(err)
		}
		return strings.TrimSpace(string(output))
	}
	if got := invoke(); got != "busy" {
		t.Fatalf("SDK lock did not exclude .NET: %q", got)
	}
	lock.Close()
	if got := invoke(); got != "acquired" {
		t.Fatalf("empty retained lock treated as busy: %q", got)
	}
}
