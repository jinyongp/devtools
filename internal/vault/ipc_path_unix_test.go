//go:build linux || darwin

package vault

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLongUnixSessionPathRetainsPrivateRootBinding(t *testing.T) {
	root := filepath.Join(t.TempDir(), strings.Repeat("long", 40))
	directory := filepath.Join(root, "vault")
	id, _ := NewID()
	wrapping, err := NewKey()
	if err != nil {
		t.Fatal(err)
	}
	defer wrapping.Destroy()
	protector := sessionTestProtector{id: id, key: wrapping}
	master, err := (KeyFileStore{Directory: directory}).Initialize(context.Background(), id, "dpapi", protector)
	if err != nil {
		t.Fatal(err)
	}
	defer master.Destroy()
	endpoint := sessionEndpoint(directory, id)
	if len(endpoint) >= 100 || filepath.Dir(endpoint) == directory || endpoint == sessionEndpoint(directory+"other", id) {
		t.Fatal("long endpoint is not short/root-specific")
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- ServeKeySession(ctx, directory) }()
	t.Cleanup(func() {
		cancel()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Error("session did not exit")
		}
	})
	deadline := time.Now().Add(5 * time.Second)
	var client *SessionClient
	for time.Now().Before(deadline) {
		client, err = OpenSessionClient(directory)
		if err == nil {
			break
		}
		select {
		case err = <-done:
			t.Fatal("session exited", err)
		case <-time.After(10 * time.Millisecond):
		}
	}
	if err != nil {
		t.Fatal("long path daemon unavailable", err)
	}
	if _, err = client.Status(ctx); err != nil {
		t.Fatal("peer-authenticated status failed", err)
	}
	info, err := os.Stat(filepath.Dir(endpoint))
	if err != nil || info.Mode().Perm() != 0700 {
		t.Fatal("short endpoint parent is not private", err)
	}
	cancel()
	select {
	case err = <-done:
		if err != nil && !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
		done <- nil
	case <-time.After(5 * time.Second):
		t.Fatal("session did not stop")
	}
	if _, err = os.Lstat(endpoint); !os.IsNotExist(err) {
		t.Fatal("socket survived session exit", err)
	}
}
