package values

import (
	"context"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jinyongp/devtools/internal/maintenance"
	"github.com/jinyongp/devtools/internal/protocol"
)

func TestNormalReadersShareMaintenanceGate(t *testing.T) {
	root, _, master, _ := vaultSnapshotFixture(t, 1)
	var calls atomic.Int32
	store := ModernStore{Directory: filepath.Join(root, "profiles"), Profile: "app", Master: snapshotMaster(master, &calls)}
	publicRoot := privateTempDir(t)
	public := ModernStore{Directory: filepath.Join(publicRoot, "profiles"), Profile: "public"}
	if _, e := public.Update(context.Background(), func(e *Editor) (bool, *protocol.Error) { return e.Set(Variable, "URL", "", "public") }); e != nil {
		t.Fatal(e)
	}
	release, err := maintenance.AcquireShared(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	// The held leases prove sharing; this deadline only bounds a lock regression.
	// Keep durable fixture writes outside it and allow loaded native runners time
	// to read. This test does not impose a latency SLA.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	snapshot, e := CaptureVault(ctx, root, "app", "", true, time.Now(), store.Master)
	if e != nil {
		t.Fatal("snapshot requested exclusive gate", e)
	}
	snapshot.Destroy()
	if _, e = store.Inspect(ctx, ""); e != nil {
		t.Fatal("inspection requested exclusive gate", e)
	}
	publicRelease, err := maintenance.AcquireShared(ctx, publicRoot)
	if err != nil {
		t.Fatal(err)
	}
	defer publicRelease()
	snapshot, e = CapturePublic(ctx, publicRoot, "public", "", true, time.Now())
	if e != nil {
		t.Fatal("public snapshot requested exclusive gate", e)
	}
	snapshot.Destroy()
}
func TestConcurrentSnapshotCreationRechecksUnderExclusiveGate(t *testing.T) {
	root, _, master, _ := vaultSnapshotFixture(t, 0)
	var calls atomic.Int32
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	start := make(chan struct{})
	ids := make(chan string, 2)
	errs := make(chan *protocol.Error, 2)
	var wg sync.WaitGroup
	for range 2 {
		wg.Go(func() {
			<-start
			s, e := CaptureVault(ctx, root, "new-profile", "", true, time.Now(), snapshotMaster(master, &calls))
			if e != nil {
				errs <- e
				return
			}
			defer s.Destroy()
			ids <- s.Metadata().Identity.ProfileID
		})
	}
	close(start)
	wg.Wait()
	close(ids)
	close(errs)
	for e := range errs {
		t.Fatal(e)
	}
	first := ""
	count := 0
	for id := range ids {
		count++
		if first == "" {
			first = id
		}
		if id != first {
			t.Fatal("creators used different incarnations")
		}
	}
	if first == "" || count != 2 {
		t.Fatal("missing snapshots")
	}
}

func TestSnapshotAndInspectionDeadlinesAreNotUserCancellation(t *testing.T) {
	root, _, master, _ := vaultSnapshotFixture(t, 1)
	var calls atomic.Int32
	store := ModernStore{Directory: filepath.Join(root, "profiles"), Profile: "app", Master: snapshotMaster(master, &calls)}
	release, err := maintenance.AcquireExclusive(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	for _, snapshot := range []bool{false, true} {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
		var e *protocol.Error
		if snapshot {
			var s *Snapshot
			s, e = CaptureVault(ctx, root, "app", "", true, time.Now(), store.Master)
			if s != nil {
				s.Destroy()
			}
		} else {
			_, e = store.Inspect(ctx, "")
		}
		cancel()
		if e == nil || e.Code != "deadline_exceeded" || e.ExitCode != 3 {
			t.Fatal("gate deadline reported as user cancellation", e)
		}
	}
}
