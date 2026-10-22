package projectfile

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/jinyongp/devtools/internal/protocol"
)

func TestIOFailureRetainsSafeStagesAndErrno(t *testing.T) {
	cause := os.NewSyscallError("flistxattr.size", &os.PathError{Op: "read", Path: "private-path-and-value", Err: syscall.EIO})
	failure := atStage(ioFailure("read.metadata", cause), "publish.read_candidate")
	public := protocol.Public(failure, "config.format")
	facts := public.Details["facts"].(map[string]any)
	if public.Code != "io_error" || public.Details["reason"] != "io_failure" || public.Details["effects"] != "none" || facts["os_code"] != uint64(syscall.EIO) || facts["stage"] != "publish.read_candidate.read.metadata.flistxattr.size" {
		t.Fatalf("I/O diagnosis lost context: code=%s details=%v", public.Code, public.Details)
	}
	encoded, err := json.Marshal(public)
	if err != nil || strings.Contains(string(encoded), "private-path-and-value") {
		t.Fatal("raw OS cause was exposed", err)
	}
}

func TestReadFailureIdentifiesOpenStage(t *testing.T) {
	_, failure := Read(filepath.Join(t.TempDir(), "absent.toml"))
	if failure == nil || failure.Code != "io_error" || failure.Details["reason"] != "not_found" {
		t.Fatal("missing project did not fail", failure)
	}
	facts := failure.Details["facts"].(map[string]any)
	if facts["stage"] != "read.open" || facts["os_code"] == nil {
		t.Fatal("missing project lost its OS diagnosis", failure.Details)
	}
}

func fileFixture(t *testing.T) (string, *Snapshot) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "devtools.toml")
	if e := os.WriteFile(path, []byte("profile='sample'\n"), 0640); e != nil {
		t.Fatal(e)
	}
	s, e := Read(path)
	if e != nil {
		t.Fatalf("read fixture: code=%s details=%v", e.Code, e.Details)
	}
	return path, s
}
func TestPublicationPreservesPermissionsAndRevisionIdentity(t *testing.T) {
	path, s := fileFixture(t)
	next, e := Publish(context.Background(), s, []byte("profile='sample'\n# change\n"))
	if e != nil {
		t.Fatal(e, e.Details)
	}
	if next.Revision == s.Revision || next.security != s.security {
		t.Fatal("revision or permissions lost")
	}
	if e := s.Recheck(); e == nil || e.Code != "config_revision_conflict" {
		t.Fatal(e)
	}
	same := filepath.Join(filepath.Dir(path), "same-bytes")
	if e := os.WriteFile(same, next.Bytes, next.Info.Mode().Perm()); e != nil {
		t.Fatal(e)
	}
	if e := copyFileForReplacement(same, path); e != nil {
		t.Fatal(e)
	}
	now, e := Read(path)
	if e != nil || now.Revision == next.Revision {
		t.Fatal("same-byte replacement reused revision", e)
	}
}
func copyFileForReplacement(src, dst string) error {
	if err := os.Rename(src, dst); err != nil {
		return err
	}
	return nil
}
func TestPublicationFailureOutcomes(t *testing.T) {
	path, s := fileFixture(t)
	before := append([]byte{}, s.Bytes...)
	_, e := publish(context.Background(), s, []byte("profile='next'\n"), publication{replace: func(context.Context, *Snapshot, *Snapshot) (bool, bool, string, error) {
		return false, false, "", os.ErrPermission
	}})
	if e == nil || e.Code != "io_error" || e.Details["effects"] != "none" {
		t.Fatal(e)
	}
	data, _ := os.ReadFile(path)
	if !bytes.Equal(data, before) {
		t.Fatal("pre-publication failure changed source")
	}
	_, e = publish(context.Background(), s, []byte("profile='next'\n"), publication{replace: func(context.Context, *Snapshot, *Snapshot) (bool, bool, string, error) {
		return false, false, "", Error("config_revision_conflict", "target_changed", nil)
	}})
	if e == nil || e.Code != "config_revision_conflict" || e.ExitCode != 3 || e.Details["effects"] != "none" {
		t.Fatal("publication retry lost its typed conflict", e)
	}
	_, e = publish(context.Background(), s, []byte("profile='next'\n"), publication{replace: func(ctx context.Context, a, b *Snapshot) (bool, bool, string, error) {
		committed, uncertain, backup, err := replace(ctx, a, b)
		if err != nil {
			return committed, uncertain, backup, err
		}
		return true, false, backup, os.NewSyscallError("directory_sync", syscall.EIO)
	}})
	if e == nil || e.Code != "config_commit_uncertain" || e.Details["effects"] != "committed" || e.Details["retry"] != "inspect_first" || e.Details["phase"] != "publish" {
		t.Fatal(e)
	}
	if facts := e.Details["facts"].(map[string]any); facts["stage"] != "publish.replace.directory_sync" || facts["os_code"] != uint64(syscall.EIO) {
		t.Fatal("committed sync failure lost its OS diagnosis", e.Details)
	}
	data, _ = os.ReadFile(path)
	if string(data) != "profile='next'\n" {
		t.Fatal("committed result was rolled back")
	}
	if err := os.WriteFile(path, []byte("profile='later'\n"), 0640); err != nil {
		t.Fatal(err)
	}
	if e = s.Recheck(); e == nil {
		t.Fatal("later writer was accepted as old state")
	}
}

func TestPostPublicationReadFailureRetainsCause(t *testing.T) {
	path, snapshot := fileFixture(t)
	_, failure := publish(context.Background(), snapshot, []byte("profile='next'\n"), publication{stage: func(stage string) {
		if stage == "published" {
			if err := os.Remove(path); err != nil {
				t.Fatal(err)
			}
		}
	}})
	if failure == nil || failure.Code != "config_commit_uncertain" || failure.Details["effects"] != "committed" || failure.Details["phase"] != "observe" || failure.Details["retry"] != "inspect_first" {
		t.Fatal("post-publication failure changed recovery semantics", failure)
	}
	public := protocol.Public(failure, "config.format")
	causes := public.Details["causes"].([]any)
	if len(causes) != 1 {
		t.Fatal("post-publication read cause was lost", public.Details)
	}
	cause := causes[0].(map[string]any)
	facts := cause["facts"].(map[string]any)
	if cause["code"] != "io_error" || cause["reason"] != "not_found" || facts["stage"] != "read.open" || facts["os_code"] == nil {
		t.Fatal("post-publication read cause lost its diagnosis", cause)
	}
}

func TestBackupCleanupFailureRetainsErrno(t *testing.T) {
	_, snapshot := fileFixture(t)
	backup := filepath.Join(t.TempDir(), "retained-backup")
	if err := os.Mkdir(backup, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(backup, "occupied"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	_, failure := publish(context.Background(), snapshot, []byte("profile='next'\n"), publication{replace: func(ctx context.Context, source, candidate *Snapshot) (bool, bool, string, error) {
		committed, uncertain, _, err := replace(ctx, source, candidate)
		return committed, uncertain, backup, err
	}})
	if failure == nil || failure.Code != "config_commit_uncertain" || failure.Details["reason"] != "backup_cleanup" || failure.Details["effects"] != "committed" {
		t.Fatal("backup cleanup did not report its committed outcome", failure)
	}
	facts := failure.Details["facts"].(map[string]any)
	if facts["stage"] != "publish.backup_cleanup" || facts["os_code"] == nil || facts["backup_name"] != "retained-backup" {
		t.Fatal("backup cleanup lost its OS diagnosis", failure.Details)
	}
}
func TestPublicationRefusesHardlinkAndReadonly(t *testing.T) {
	path, s := fileFixture(t)
	if e := os.Link(path, path+".link"); e != nil {
		t.Fatal(e)
	}
	if e := CheckWritable(s); e == nil || e.Code != "config_unsafe_target" {
		t.Fatal(e)
	}
	if e := os.Remove(path + ".link"); e != nil {
		t.Fatal(e)
	}
	if e := os.Chmod(path, 0400); e != nil {
		t.Fatal(e)
	}
	defer os.Chmod(path, 0640)
	if e := CheckWritable(s); e == nil {
		t.Fatal("read-only source accepted")
	}
}

func TestPublicationDetectsSameBytesReplacementAfterPublish(t *testing.T) {
	path, snapshot := fileFixture(t)
	data := []byte("profile='next'\n")
	_, failure := publish(context.Background(), snapshot, data, publication{replace: func(ctx context.Context, source, candidate *Snapshot) (bool, bool, string, error) {
		committed, uncertain, backup, err := replace(ctx, source, candidate)
		if err != nil {
			return committed, uncertain, backup, err
		}
		later := filepath.Join(filepath.Dir(path), "later-writer")
		if err := os.WriteFile(later, data, snapshot.Info.Mode().Perm()); err != nil {
			t.Fatal(err)
		}
		if err := os.Rename(later, path); err != nil {
			t.Fatal(err)
		}
		return committed, uncertain, backup, nil
	}})
	if failure == nil || failure.Code != "config_commit_uncertain" || failure.Details["effects"] != "committed" || failure.Details["phase"] != "observe" {
		t.Fatal("later writer's identical bytes were mistaken for our candidate", failure)
	}
	if observed, err := os.ReadFile(path); err != nil || !bytes.Equal(observed, data) {
		t.Fatal("later writer was not preserved", err)
	}
}

func TestProjectfileCrashHelper(t *testing.T) {
	stage := os.Getenv("DEVTOOLS_TEST_CONFIG_CRASH_STAGE")
	if stage == "" {
		return
	}
	path := os.Getenv("DEVTOOLS_TEST_CONFIG_CRASH_PATH")
	s, e := Read(path)
	if e != nil {
		t.Fatal(e)
	}
	_, e = publish(context.Background(), s, []byte("profile='next'\n"), publication{stage: func(current string) {
		if current == stage {
			_, _ = os.Stdout.WriteString("ready\n")
			time.Sleep(time.Hour)
		}
	}})
	if e != nil {
		t.Fatal(e)
	}
}
func TestKilledPublisherLeavesCompleteSourceAndPreservesLaterWriter(t *testing.T) {
	for _, stage := range []string{"created", "written", "flushed", "before_publish", "published"} {
		t.Run(stage, func(t *testing.T) {
			path, _ := fileFixture(t)
			exe, e := os.Executable()
			if e != nil {
				t.Fatal(e)
			}
			cmd := exec.Command(exe, "-test.run=^TestProjectfileCrashHelper$")
			cmd.Env = append(os.Environ(), "DEVTOOLS_TEST_CONFIG_CRASH_STAGE="+stage, "DEVTOOLS_TEST_CONFIG_CRASH_PATH="+path)
			stdout, e := cmd.StdoutPipe()
			if e != nil {
				t.Fatal(e)
			}
			var stderr bytes.Buffer
			cmd.Stderr = &stderr
			if e = cmd.Start(); e != nil {
				t.Fatal(e)
			}
			done := make(chan bool, 1)
			go func() { scanner := bufio.NewScanner(stdout); done <- scanner.Scan() && scanner.Text() == "ready" }()
			select {
			case ready := <-done:
				if !ready {
					_ = cmd.Process.Kill()
					_ = cmd.Wait()
					t.Fatal("publisher exited before crash boundary", stderr.String())
				}
			case <-time.After(20 * time.Second):
				_ = cmd.Process.Kill()
				_ = cmd.Wait()
				t.Fatal("publisher did not reach crash boundary", stderr.String())
			}
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
			data, e := os.ReadFile(path)
			if e != nil {
				t.Fatal(e)
			}
			want := "profile='sample'\n"
			if stage == "published" {
				want = "profile='next'\n"
			}
			if string(data) != want {
				t.Fatalf("incomplete or wrong source: %q", data)
			}
			later := []byte("profile='later'\n")
			if e = os.WriteFile(path, later, 0640); e != nil {
				t.Fatal(e)
			}
			now, pe := Read(path)
			if pe != nil || !bytes.Equal(now.Bytes, later) {
				t.Fatal("orphan state overwrote later writer", pe)
			}
		})
	}
}
