package projectfile

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func fileFixture(t *testing.T) (string, *Snapshot) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "devtools.toml")
	if e := os.WriteFile(path, []byte("profile='sample'\n"), 0640); e != nil {
		t.Fatal(e)
	}
	s, e := Read(path)
	if e != nil {
		t.Fatal(e)
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
		return true, false, backup, errors.New("injected post-publication sync failure")
	}})
	if e == nil || e.Code != "config_commit_uncertain" || e.Details["effects"] != "committed" || e.Details["retry"] != "inspect_first" || e.Details["phase"] != "publish" {
		t.Fatal(e)
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
