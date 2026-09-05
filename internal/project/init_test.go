package project

import (
	"os"
	"path/filepath"
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
