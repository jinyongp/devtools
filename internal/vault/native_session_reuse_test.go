package vault

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type countedNativeProtector struct {
	Protector
	calls *atomic.Int32
}

func (p countedNativeProtector) Unwrap(ctx context.Context, b []byte) ([]byte, error) {
	p.calls.Add(1)
	return p.Protector.Unwrap(ctx, b)
}

// Counts actual logical native unwrap/helper requests, not OS syscalls. The
// wrapped implementation is unchanged and invokes the real native API in the
// normal bounded helper. Initialization's wrap/verification is excluded.
func checkNativeSessionReuse(t *testing.T, backend string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	id, err := NewID()
	if err != nil {
		t.Fatal(err)
	}
	protector, err := nativeProtector(backend, id, false)
	if err != nil {
		t.Fatal(err)
	}
	if removable, ok := protector.(interface{ Remove(context.Context) error }); ok {
		t.Cleanup(func() {
			if e := removable.Remove(context.Background()); e != nil && !errors.Is(e, ErrKeyMissing) {
				t.Error("owned native item cleanup", e)
			}
		})
	}
	directory := filepath.Join(privateTempDir(t), "vault")
	key, err := (KeyFileStore{Directory: directory}).Initialize(ctx, id, backend, protector)
	if err != nil {
		t.Fatal(err)
	}
	defer key.Destroy()
	s := NewKeySession(directory)
	defer s.Close()
	var calls atomic.Int32
	s.protector = func(k KeyFile, p *Material, interactive bool) (Protector, error) {
		actual, e := sessionProtector(k, p, interactive)
		if e != nil {
			return nil, e
		}
		return countedNativeProtector{actual, &calls}, nil
	}
	base := time.Now()
	var offset atomic.Int64
	s.now = func() time.Time { return base.Add(time.Duration(offset.Load())) }
	use := func() error {
		got, e := s.Use(ctx)
		if e != nil {
			return e
		}
		defer got.Destroy()
		if !sameMaterial(got, key) {
			return ErrIntegrity
		}
		return nil
	}
	if e := use(); e != nil || calls.Load() != 1 {
		t.Fatal("actual cold native unwrap", e, calls.Load())
	}
	for _, n := range []int{10, 100, 1000} {
		for i := 0; i < n; i++ {
			if e := use(); e != nil {
				t.Fatal("warm native session", e)
			}
		}
		if calls.Load() != 1 {
			t.Fatal("warm use called native API", n, calls.Load())
		}
	}
	var group sync.WaitGroup
	failures := make(chan error, 32)
	for i := 0; i < 32; i++ {
		group.Add(1)
		go func() { defer group.Done(); failures <- use() }()
	}
	group.Wait()
	close(failures)
	for e := range failures {
		if e != nil {
			t.Fatal("concurrent warm use", e)
		}
	}
	if calls.Load() != 1 {
		t.Fatal("concurrent warm use unwrapped", calls.Load())
	}
	offset.Store(int64(DefaultIdleTimeout + time.Second))
	if got := s.Status(); got.State != Locked || calls.Load() != 1 {
		t.Fatal("expiry inspection performed native unwrap", got.State, calls.Load())
	}
	if e := use(); e != nil || calls.Load() != 2 {
		t.Fatal("actual cold use after expiry", e, calls.Load())
	}
	if e := s.Lock(ctx); e != nil {
		t.Fatal(e)
	}
	if _, e := s.Use(ctx); !errors.Is(e, ErrLocked) || calls.Load() != 2 {
		t.Fatal("manual lock implicitly unwrapped", e, calls.Load())
	}
	if e := s.Unlock(ctx, nil, false); e != nil || calls.Load() != 3 {
		t.Fatal("explicit native unlock", e, calls.Load())
	}
	if e := use(); e != nil || calls.Load() != 3 {
		t.Fatal("warm use after explicit unlock", e, calls.Load())
	}
	t.Logf(`native_session_counts={"backend":%q,"initial_cold_unwraps":1,"warm_use_batches":[10,100,1000],"concurrent_warm_uses":32,"warm_unwraps":0,"after_expiry_unwraps":1,"manual_locked_use_unwraps":0,"explicit_unlock_unwraps":1,"total_unwraps":%d,"initialization_excluded":true,"os_syscall_count":false}`, backend, calls.Load())
}
