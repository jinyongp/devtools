package valuehandoff

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/jinyongp/devtools/internal/backup"
	"github.com/jinyongp/devtools/internal/protocol"
	"github.com/jinyongp/devtools/internal/values"
	"github.com/jinyongp/devtools/internal/vault"
)

func TestLogicalBackupRestoreExcludesRuntimeAndFencesPendingDelivery(t *testing.T) {
	var effects atomic.Int32
	s, _, approved := deliveryFixture(t, Receiver{Handle: func(context.Context, *ReceivedInput, io.Writer, io.Writer) error { effects.Add(1); return nil }})
	ctx := context.Background()
	a, err := s.access(ctx, false)
	if err != nil {
		t.Fatal(err)
	}
	defer a.master.Destroy()
	parent := filepath.Dir(s.Root)
	identity, recipient := filepath.Join(parent, "identity"), filepath.Join(parent, "recipient")
	if _, e := backup.Keygen(identity, recipient); e != nil {
		t.Fatal(e)
	}
	engine := backup.Engine{Data: s.Root, Config: filepath.Join(parent, "backup-config"), Cache: filepath.Join(parent, "backup-cache"), VaultMaster: func(context.Context) (*vault.Material, error) {
		b := a.master.CopyBytes()
		defer clear(b)
		return vault.NewMaterial(b), nil
	}}
	archive := filepath.Join(parent, "logical.age")
	if _, e := engine.Create(ctx, s.Profile, archive, recipient); e != nil {
		t.Fatal(e)
	}
	store := values.CurrentStore(filepath.Join(s.Root, "profiles"), s.Profile)
	if _, e := store.Update(ctx, func(ed *values.Editor) (bool, *protocol.Error) {
		return ed.Set(values.Secret, "TOKEN", "", "changed-after-backup")
	}); e != nil {
		t.Fatal(e)
	}
	request := backup.ImportRequest{Path: archive, IdentityPath: identity, Target: s.Profile, Replace: true, RecoveryDirectory: filepath.Join(s.Root, "recovery")}
	preview, e := engine.Import(ctx, request)
	if e != nil {
		t.Fatal("preview", e.Code, e.Details)
	}
	request.Expected, request.RequestID = preview.Plan.Digest, requestID(820)
	if _, e := engine.Import(ctx, request); e != nil {
		t.Fatal("apply", e.Code, e.Details)
	}
	if _, e := s.Deliver(ctx, approved.Item.ID, requestID(821), RevisionInput{2}); e == nil || e.Code != vault.ErrInputChanged.Error() {
		t.Fatal("restore reused pending authority", e)
	}
	if effects.Load() != 0 {
		t.Fatal("restore delivered old input")
	}
	// Import into another fresh protected root to prove grants/registrations and
	// pending ciphertext do not travel with the logical archive.
	target, _ := fixture(t)
	targetAccess, err := target.access(ctx, false)
	if err != nil {
		t.Fatal(err)
	}
	defer targetAccess.master.Destroy()
	targetEngine := backup.Engine{Data: target.Root, Config: filepath.Join(filepath.Dir(target.Root), "config"), Cache: filepath.Join(filepath.Dir(target.Root), "cache"), VaultMaster: func(context.Context) (*vault.Material, error) {
		b := targetAccess.master.CopyBytes()
		defer clear(b)
		return vault.NewMaterial(b), nil
	}}
	copyRequest := backup.ImportRequest{Path: archive, IdentityPath: identity, Target: "copy"}
	copyPreview, e := targetEngine.Import(ctx, copyRequest)
	if e != nil {
		t.Fatal(e)
	}
	copyRequest.Expected, copyRequest.RequestID = copyPreview.Plan.Digest, requestID(822)
	if _, e := targetEngine.Import(ctx, copyRequest); e != nil {
		t.Fatal(e)
	}
	for _, domain := range []string{"value-executions", "value-execution-payloads", "value-execution-requests", "value-runners", "value-runner-requests", "value-runner-transports"} {
		if _, err := os.Stat(filepath.Join(target.Root, domain)); !os.IsNotExist(err) {
			t.Fatal("portable archive transferred runtime state", domain, err)
		}
	}
}
