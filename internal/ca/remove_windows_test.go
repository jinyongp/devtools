package ca

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestWindowsCAMaterialRemovalRequiresSelectedIdentity(t *testing.T) {
	store := Store{Data: filepath.Join(t.TempDir(), "data")}
	authority, _, failure := store.Init(context.Background())
	if failure != nil {
		t.Fatal(failure)
	}
	path := filepath.Join(store.root(), "material.json")
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if failure = store.Remove(context.Background(), "wrong-fingerprint"); failure == nil || failure.Code != "ca_changed" {
		t.Fatal("wrong CA deletion accepted", failure)
	}
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("wrong CA request changed keys", err)
	}
	canceled, stop := context.WithCancel(context.Background())
	stop()
	if failure = store.Remove(canceled, authority.Fingerprint()); failure == nil {
		t.Fatal("canceled removal succeeded")
	}
	after, err = os.ReadFile(path)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("canceled removal changed keys", err)
	}
	if failure = store.Remove(context.Background(), authority.Fingerprint()); failure != nil {
		t.Fatal(failure)
	}
	if _, err = os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("selected CA material retained", err)
	}
	if failure = store.Remove(context.Background(), authority.Fingerprint()); failure != nil {
		t.Fatal("replayed removal failed", failure)
	}
}
