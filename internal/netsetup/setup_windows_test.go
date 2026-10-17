package netsetup

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"github.com/jinyongp/devtools/internal/platformfs"
	"github.com/jinyongp/devtools/internal/protocol"
	"github.com/jinyongp/devtools/internal/tasks"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func windowsSetupFixture(t *testing.T) (Manager, string) {
	t.Helper()
	root := t.TempDir()
	path := filepath.Join(root, "devtools.exe")
	if err := os.WriteFile(path, []byte("image-fixture"), 0755); err != nil {
		t.Fatal(err)
	}
	data := filepath.Join(root, "data")
	r, err := platformfs.OpenRoot(data, true)
	if err != nil {
		t.Fatal(err)
	}
	r.Close()
	m := Manager{Data: data, Executable: path, Run: func(context.Context, string, any, bool) *protocol.Error {
		t.Fatal("native mode invoked OS permission helper")
		return nil
	}}
	return m, path
}
func TestWindowsNativeSetupReplayWithoutSystemMutation(t *testing.T) {
	m, path := windowsSetupFixture(t)
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	id := tasks.ID()
	if _, e := m.Apply(context.Background(), id, true); e != nil {
		t.Fatal(e)
	}
	m.Executable = strings.ToLower(path)
	if _, e := m.Apply(context.Background(), id, true); e != nil {
		t.Fatal("same-image path alias changed request identity", e)
	}
	report, e := m.Check()
	if e != nil || report["mode"] != "native" || report["enabled"] != false || report["intent_enabled"] != true || report["recovery_required"] != false || report["bind_check"] != "performed_when_listener_starts" {
		t.Fatalf("native setup report: %#v %v", report, e)
	}
	after, err := os.ReadFile(path)
	if err != nil || sha256.Sum256(before) != sha256.Sum256(after) {
		t.Fatal("native setup changed executable", err)
	}
	if _, e = m.Apply(context.Background(), id, false); e == nil || e.Code != "request_conflict" {
		t.Fatalf("changed request replay accepted: %v", e)
	}
	m.Isolated = true
	if _, e = m.Apply(context.Background(), tasks.ID(), true); e == nil || e.Code != "isolated_operation_not_allowed" {
		t.Fatal(e)
	}
}
func TestWindowsExecutableIdentityRetainsImageUntilClose(t *testing.T) {
	_, path := windowsSetupFixture(t)
	f, identity, e := OpenIdentity(path)
	if e != nil {
		t.Fatal(e)
	}
	defer f.Close()
	want := sha256.Sum256([]byte("image-fixture"))
	if identity.Path == "" || identity.Inode == 0 || identity.Digest != hex.EncodeToString(want[:]) {
		t.Fatal(identity)
	}
	if err := os.Rename(path, path+".moved"); err == nil {
		t.Fatal("held executable identity could move")
	}
	if err := os.WriteFile(path, []byte("replacement"), 0755); err == nil {
		t.Fatal("held executable identity could change")
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("replacement"), 0755); err != nil {
		t.Fatal(err)
	}
	next, after, e := OpenIdentity(path)
	if e != nil {
		t.Fatal(e)
	}
	next.Close()
	if after == identity || after.Digest == identity.Digest {
		t.Fatal("changed image retained identity")
	}
}
func TestWindowsNativeSetupDoesNotMutateCapabilities(t *testing.T) {
	_, path := windowsSetupFixture(t)
	f, identity, e := OpenIdentity(path)
	if e != nil {
		t.Fatal(e)
	}
	f.Close()
	if e = SystemApply(context.Background(), Request{Identity: identity, Enable: true}, path); e == nil || e.Code != "unsupported_platform" {
		t.Fatalf("native capability mutation accepted: %v", e)
	}
	for _, path := range []string{`C:relative.exe`, `\\server\share\devtools.exe`} {
		if f, _, e := OpenIdentity(path); e == nil {
			f.Close()
			t.Fatal("ambiguous executable accepted")
		}
	}
}
