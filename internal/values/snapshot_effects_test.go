package values

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

func TestNewProfileRejectsInvalidSelectionBeforePublication(t *testing.T) {
	root, store, master, _ := vaultSnapshotFixture(t, 0)
	var calls atomic.Int32
	for _, selection := range []struct{ env, bundle, code string }{{"missing", "all", "env_not_found"}, {"", "missing", "bundle_not_found"}} {
		s, e := CaptureVault(context.Background(), root, "not-created", selection.env, true, time.Now(), snapshotMaster(master, &calls), selection.bundle)
		if s != nil || e == nil || e.Code != selection.code {
			t.Fatal(s, e)
		}
		p, k, err := store.ReadAnyWithMasterHeld(context.Background(), "not-created", master)
		k.Destroy()
		if p != nil || err == nil || err.Error() != "profile_not_found" {
			t.Fatal("failed selection published logical state", p, err)
		}
	}
	s, e := CaptureVault(context.Background(), root, "valid-new", "", true, time.Now(), snapshotMaster(master, &calls), "all")
	if e != nil {
		t.Fatal(e)
	}
	defer s.Destroy()
	if !s.StateCopy().PreparationChanged() {
		t.Fatal("new profile effect was lost")
	}
	next, e := CaptureVault(context.Background(), root, "valid-new", "", true, time.Now(), snapshotMaster(master, &calls), "all")
	if e != nil {
		t.Fatal(e)
	}
	defer next.Destroy()
	if next.StateCopy().PreparationChanged() || s.Metadata().Identity != next.Metadata().Identity {
		t.Fatal("existing profile changed or identity unstable")
	}
}
