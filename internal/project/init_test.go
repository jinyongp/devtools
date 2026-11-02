package project

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestInit(t *testing.T) {
	root := t.TempDir()
	got, err := Init(root, "myapp")
	if err != nil || !got.Created || got.Profile != "myapp" {
		t.Fatalf("create: %+v %v", got, err)
	}
	// Formatting and comments must survive an idempotent retry.
	content := "# keep this comment\nprofile = 'myapp'\n"
	write(t, got.ConfigPath, content)
	again, err := Init(root, "myapp")
	if err != nil || again.Created {
		t.Fatalf("retry: %+v %v", again, err)
	}
	_, err = Init(root, "other")
	if err == nil || err.Code != "profile_conflict" {
		t.Fatalf("conflict: %v", err)
	}
	data, readErr := os.ReadFile(got.ConfigPath)
	if readErr != nil || string(data) != content {
		t.Fatal("changed existing configuration")
	}
	write(t, got.ConfigPath, "broken = [")
	_, err = Init(root, "myapp")
	if err == nil || err.Code != "invalid_config" {
		t.Fatalf("malformed: %v", err)
	}
	data, _ = os.ReadFile(got.ConfigPath)
	if string(data) != "broken = [" {
		t.Fatal("overwrote malformed config")
	}
}

func TestInitCanonicalDirectory(t *testing.T) {
	base := t.TempDir()
	actual := filepath.Join(base, "actual")
	if err := os.Mkdir(actual, 0700); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(base, "alias")
	if err := os.Symlink(actual, alias); err != nil {
		t.Fatal(err)
	}
	canonical, err := filepath.EvalSymlinks(actual)
	if err != nil {
		t.Fatal(err)
	}
	got, initErr := Init(alias, "linked")
	if initErr != nil || !got.Created || got.ConfigPath != filepath.Join(canonical, Filename) {
		t.Fatalf("canonical init: %+v %v", got, initErr)
	}
}

func TestConcurrentInit(t *testing.T) {
	root := t.TempDir()
	var group sync.WaitGroup
	created := make(chan bool, 12)
	for range 12 {
		group.Go(func() {
			got, err := Init(root, "shared")
			if err != nil {
				t.Errorf("concurrent init: %v", err)
				return
			}
			created <- got.Created
		})
	}
	group.Wait()
	close(created)
	count := 0
	for value := range created {
		if value {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("created %d times", count)
	}
	files, err := os.ReadDir(root)
	if err != nil || len(files) != 1 || files[0].Name() != Filename {
		t.Fatalf("temporary files left: %v %v", files, err)
	}
}

func TestInitInvalidInputAndSymlink(t *testing.T) {
	root := t.TempDir()
	_, err := Init(root, "../bad")
	if err == nil || err.Code != "invalid_argument" {
		t.Fatal(err)
	}
	files, _ := os.ReadDir(root)
	if len(files) != 0 {
		t.Fatal("invalid input wrote files")
	}
	target := filepath.Join(root, "target")
	write(t, target, "profile='myapp'\n")
	if err := os.Symlink(target, filepath.Join(root, Filename)); err != nil {
		t.Fatal(err)
	}
	_, err = Init(root, "myapp")
	if err == nil || err.Code != "invalid_config" {
		t.Fatalf("symlink: %v", err)
	}
}

func TestInitInferredNameBoundaries(t *testing.T) {
	for _, name := range []string{"App_1.test-web", strings.Repeat("a", 128), strings.Repeat("b", 129), "한글", "-leading", ".leading"} {
		t.Run(name, func(t *testing.T) {
			directory := filepath.Join(t.TempDir(), name)
			if err := os.Mkdir(directory, 0700); err != nil {
				t.Fatal(err)
			}
			selected, failure := InspectInit(directory, "", false)
			if failure != nil || selected.Profile != name || selected.ProfileSource != "directory" {
				t.Fatalf("%+v %v", selected, failure)
			}
			_, failure = PublishInit(directory, selected, false)
			valid := name == "App_1.test-web" || name == strings.Repeat("a", 128)
			if valid && failure != nil || !valid && (failure == nil || failure.Code != "invalid_argument") {
				t.Fatalf("name=%q failure=%v", name, failure)
			}
			if !valid {
				if entries, err := os.ReadDir(directory); err != nil || len(entries) != 0 {
					t.Fatal("invalid inferred name wrote files")
				}
			}
		})
	}
	// Inspect only: no attempted publication into the host filesystem root.
	current, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.VolumeName(current) + string(os.PathSeparator)
	selected, failure := InspectInit(root, "", false)
	if failure == nil && !selected.Existing && selected.Profile != "" {
		t.Fatalf("filesystem root inferred a name: %+v", selected)
	}
}

func TestConcurrentImplicitInitAdoptsPublishedConfiguration(t *testing.T) {
	directory := t.TempDir()
	var group sync.WaitGroup
	results := make(chan InitResult, 2)
	for _, profile := range []string{"first", "second"} {
		group.Go(func() {
			result, failure := PublishInit(directory, InitResult{Profile: profile, ProfileSource: "prompt", ProfilePresence: "absent"}, false)
			if failure != nil {
				t.Errorf("concurrent init: %v", failure)
				return
			}
			results <- result
		})
	}
	group.Wait()
	close(results)
	created, profile := 0, ""
	for result := range results {
		if result.Created {
			created++
		}
		if profile != "" && result.Profile != profile {
			t.Fatal("implicit initializer returned an unpublished profile")
		}
		profile = result.Profile
	}
	if created != 1 {
		t.Fatalf("created %d configurations", created)
	}
}
