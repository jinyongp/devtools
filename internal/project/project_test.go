package project

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
}

func TestResolve(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, Filename), "profile = 'parent'\n")
	nested := filepath.Join(root, "repo")
	if err := os.MkdirAll(filepath.Join(nested, "src"), 0700); err != nil {
		t.Fatal(err)
	}
	got, err := Resolve(filepath.Join(nested, "src"), "")
	if err != nil || got.Profile != "parent" {
		t.Fatalf("upward lookup: %+v %v", got, err)
	}
	write(t, filepath.Join(nested, ".git"), "gitdir: elsewhere\n")
	_, err = Resolve(filepath.Join(nested, "src"), "")
	if err == nil || err.Code != "project_not_found" {
		t.Fatalf("crossed worktree boundary: %v", err)
	}
	write(t, filepath.Join(nested, Filename), "profile = 'nested'\n")
	got, err = Resolve(filepath.Join(nested, "src"), "")
	if err != nil || got.Profile != "nested" || got.Source != "file" {
		t.Fatalf("nearest config: %+v %v", got, err)
	}
	write(t, filepath.Join(nested, Filename), "broken = [")
	got, err = Resolve(nested, "explicit")
	if err != nil || got.Profile != "explicit" || got.ConfigPath != "" {
		t.Fatalf("explicit precedence: %+v %v", got, err)
	}
	_, err = Resolve(nested, "")
	if err == nil || err.Code != "invalid_config" {
		t.Fatalf("invalid config fell back: %v", err)
	}
}

func TestInvalidConfig(t *testing.T) {
	for _, input := range []string{"", "profile='../x'", "profile='x'\nextra=true", "profile=1", "profile='x'\nprofile='y'"} {
		t.Run(input, func(t *testing.T) {
			_, err := parse([]byte(input), "/project/devtools.toml", "/project")
			if err == nil || err.Code != "invalid_config" || err.Details["path"] != "/project/devtools.toml" {
				t.Fatalf("expected config error: %v", err)
			}
		})
	}
}

func TestTrackedConfigInWorktree(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Fatal("git required for worktree integration test")
	}
	base := t.TempDir()
	repo := filepath.Join(base, "repo")
	if err := os.Mkdir(repo, 0700); err != nil {
		t.Fatal(err)
	}
	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", repo}, args...)...)
		cmd.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %s: %v", args, out, err)
		}
	}
	git("init", "--quiet")
	write(t, filepath.Join(repo, Filename), "profile='shared'\n")
	git("add", Filename)
	git("-c", "user.name=Test", "-c", "user.email=test@example.com", "-c", "commit.gpgsign=false", "commit", "--quiet", "-m", "fixture")
	worktree := filepath.Join(base, "worktree")
	git("worktree", "add", "--quiet", "--detach", worktree)
	for _, dir := range []string{repo, worktree} {
		got, err := Resolve(dir, "")
		if err != nil || got.Profile != "shared" {
			t.Fatalf("%s: %+v %v", dir, got, err)
		}
		canonical, err2 := filepath.EvalSymlinks(dir)
		if err2 != nil || got.ConfigPath != filepath.Join(canonical, Filename) {
			t.Fatalf("wrong config path: %+v", got)
		}
	}
}

func TestSymlinkAndMissingDirectory(t *testing.T) {
	root := t.TempDir()
	actual := filepath.Join(root, "actual")
	if err := os.Mkdir(actual, 0700); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(actual, Filename), "profile='linked'\n")
	link := filepath.Join(root, "link")
	if err := os.Symlink(actual, link); err != nil {
		t.Fatal(err)
	}
	got, err := Resolve(link, "")
	if err != nil || got.Profile != "linked" {
		t.Fatalf("symlink: %+v %v", got, err)
	}
	_, err = Resolve(filepath.Join(root, "missing"), "")
	if err == nil || err.Code != "io_error" {
		t.Fatalf("missing dir: %v", err)
	}
}
