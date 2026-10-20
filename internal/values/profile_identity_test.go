package values

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jinyongp/devtools/internal/maintenance"
	"github.com/jinyongp/devtools/internal/profilekey"
	"github.com/jinyongp/devtools/internal/protocol"
	"github.com/jinyongp/devtools/internal/vault"
)

func TestModernPublicProfileNameMustMatchSelectedFile(t *testing.T) {
	ctx := context.Background()
	root := privateTempDir(t)
	s := ModernStore{Directory: filepath.Join(root, "profiles"), Profile: "selected"}
	// Keep the canonical identity/legacy marker pair valid so only the internal
	// document name is wrong.
	if _, e := s.Update(ctx, func(e *Editor) (bool, *protocol.Error) { return e.Set(Variable, "URL", "", "selected-source") }); e != nil {
		t.Fatal(e)
	}
	wrong, err := vault.NewProfile("other")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = wrong.Set("URL", vault.Public, "", "belongs-to-other", nil); err != nil {
		t.Fatal(err)
	}
	raw, err := wrong.Encode()
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(s.Directory, profilekey.CanonicalFilename(s.Profile))
	if err = maintenance.Write(path, raw); err != nil {
		t.Fatal(err)
	}
	check := func(e *protocol.Error) {
		t.Helper()
		if e == nil || (e.Code != vault.ErrRecovery.Error() && e.Code != "storage_error") {
			t.Fatalf("misfiled profile was not rejected at storage boundary: %#v", e)
		}
	}
	called := false
	_, e := s.Update(ctx, func(e *Editor) (bool, *protocol.Error) {
		called = true
		return e.Set(Variable, "URL", "", "must-not-publish")
	})
	if called {
		t.Fatal("invalid identity reached mutation callback")
	}
	check(e)
	_, e = s.Inspect(ctx, "")
	check(e)
	text := "must-not-publish"
	_, e = s.Apply(ctx, Change{Action: "variable.set", Key: "URL", Value: &text, Revision: editRevision(s.Profile, wrong, true), RequestID: "00000000-0000-4000-8000-000000000511"})
	check(e)
	snapshot, e := CapturePublic(ctx, root, s.Profile, "", true, time.Now())
	if snapshot != nil {
		snapshot.Destroy()
	}
	check(e)
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(raw, after) {
		t.Fatal("misfiled source overwritten", err)
	}
}
