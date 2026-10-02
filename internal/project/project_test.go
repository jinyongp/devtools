package project

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
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
	canonical, canonicalErr := filepath.EvalSymlinks(actual)
	if err != nil || canonicalErr != nil || got.Profile != "linked" || got.Root != canonical || got.ConfigPath != filepath.Join(canonical, Filename) {
		t.Fatalf("symlink: %+v %v canonical=%q canonicalErr=%v", got, err, canonical, canonicalErr)
	}
	_, err = Resolve(filepath.Join(root, "missing"), "")
	if err == nil || err.Code != "io_error" {
		t.Fatalf("missing dir: %v", err)
	}
}

func TestCommandDefinitions(t *testing.T) {
	valid := []byte("profile='app'\n[commands.test]\nexec=['go','test','./...']\ninject=true\nenv='test'\n")
	got, err := parse(valid, "/project/devtools.toml", "/project")
	if err != nil || !got.Commands["test"].Inject || got.Commands["test"].Env != "test" {
		t.Fatalf("%+v %v", got, err)
	}
	for _, definition := range []string{"exec=[]", "exec=['']", "exec='go'", "exec=['go']\nenv='../bad'", "exec=['go']\ninject='yes'", "exec=['go']\nunknown=true"} {
		_, err := parse([]byte("profile='app'\n[commands.test]\n"+definition), "/project/devtools.toml", "/project")
		if err == nil || err.Code != "invalid_config" {
			t.Fatalf("accepted %s: %v", definition, err)
		}
	}
}

func TestCommandNameConfiguration(t *testing.T) {
	cases := []struct {
		name  string
		valid bool
	}{
		{"docs:dev", true}, {"docs.dev", true}, {"@docs/dev", true},
		{"docs+dev", true}, {"docs=dev", true}, {"docs dev", true},
		{"文書:開発", true}, {"docs'\"$;`dev", true}, {"/^docs:/", true},
		{"_docs", true}, {strings.Repeat("文", 128), true},
		{"", false}, {"-docs", false}, {"docs\ndev", false},
		{"docs\rdev", false}, {"docs\tdev", false}, {"docs\x00dev", false},
		{"docs\x1bdev", false}, {"docs\x7fdev", false}, {"docs\u0085dev", false},
		{strings.Repeat("x", 129), false}, {strings.Repeat("文", 129), false},
	}
	for _, tc := range cases {
		t.Run(fmt.Sprintf("%q", tc.name), func(t *testing.T) {
			key, _ := json.Marshal(tc.name)
			config := fmt.Sprintf("profile='app'\n[commands.%s]\nexec=['printf','ok']\n", key)
			p, err := parse([]byte(config), "/project/devtools.toml", "/project")
			if tc.valid {
				if err != nil || len(p.Commands) != 1 || len(p.Commands[tc.name].Exec) != 2 {
					t.Fatalf("command key was not preserved: %+v %v", p, err)
				}
			} else if err == nil || err.Code != "invalid_config" {
				t.Fatalf("invalid command key was accepted: %v", err)
			}
		})
	}
	for _, config := range []string{
		"profile='docs:dev'",
		"profile='app'\n[commands.\"docs:dev\"]\nexec=['printf','ok']\nenv='docs:dev'",
		"profile='app'\n[ports.\"docs:dev\"]\nport=3000\nrange=[3000,3099]\n",
	} {
		if _, err := parse([]byte(config), "/project/devtools.toml", "/project"); err == nil {
			t.Fatalf("command name rules leaked to other identifiers: %s", config)
		}
	}
}
