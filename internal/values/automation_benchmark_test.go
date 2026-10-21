//go:build automationbench

package values

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/jinyongp/devtools/internal/maintenance"
	"github.com/jinyongp/devtools/internal/platformfs"
	"github.com/jinyongp/devtools/internal/protocol"
	"github.com/jinyongp/devtools/internal/vault"
)

func automationMutationID(n int) string { return fmt.Sprintf("00000000-0000-4000-8000-%012x", n) }

// Every background receipt is an authenticated, recipe-bound no-op on the
// current exact slot. Fixture generation and native/session setup are untimed.
func automationMutationFixture(b *testing.B, size int, mode string) (ModernStore, Change, *vault.SessionClient, string) {
	b.Helper()
	ctx := context.Background()
	base, err := filepath.EvalSymlinks(b.TempDir())
	if err != nil {
		b.Fatal(err)
	}
	root := filepath.Join(base, "data")
	vid, _ := vault.NewID()
	pass := vault.NewMaterial([]byte("synthetic-automation-benchmark-unlock"))
	defer pass.Destroy()
	ks := vault.KeyFileStore{Directory: filepath.Join(root, "vault")}
	master, err := ks.Initialize(ctx, vid, "passphrase", vault.PassphraseProtector{VaultID: vid, Passphrase: pass})
	if err != nil {
		b.Fatal(err)
	}
	defer master.Destroy()
	key, err := vault.NewKey()
	if err != nil {
		b.Fatal(err)
	}
	defer key.Destroy()
	p, err := vault.NewSealedProfile("automation", vid, master, key)
	if err != nil {
		b.Fatal(err)
	}
	if _, err := p.Set("TOKEN", vault.Secret, "", "before", key); err != nil {
		b.Fatal(err)
	}
	fs := vault.FileStore{Root: root, VaultID: vid, CatalogKey: master}
	if err := fs.Initialize(ctx); err != nil {
		b.Fatal(err)
	}
	if err := fs.Create(ctx, p, key); err != nil {
		b.Fatal(err)
	}
	client, err := (vault.SessionManager{Directory: ks.Directory}).Client(ctx)
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() {
		ctx, stop := context.WithTimeout(context.Background(), 5*time.Second)
		defer stop()
		if err := client.Retire(ctx); err != nil {
			b.Error(err)
		}
	})
	if err := client.Unlock(ctx, pass, false); err != nil {
		b.Fatal(err)
	}
	s := CurrentStore(filepath.Join(root, "profiles"), p.Name)
	view, e := s.Inspect(ctx, "")
	if e != nil {
		b.Fatal(e)
	}
	value := "first"
	request := Change{Action: "secret.set", Key: "TOKEN", Value: &value, Revision: view.Revision, RequestID: automationMutationID(0)}
	first, e := s.Apply(ctx, request)
	if e != nil || !first.Changed {
		b.Fatal("target receipt setup", e)
	}
	current := "first"
	if mode == "replay" {
		if _, e := s.Update(ctx, func(ed *Editor) (bool, *protocol.Error) { return ed.Set(Secret, "TOKEN", "", "later") }); e != nil {
			b.Fatal(e)
		}
		current = "later"
	}
	view, e = s.Inspect(ctx, "")
	if e != nil {
		b.Fatal(e)
	}
	release, err := maintenance.AcquireExclusive(ctx, root)
	if err != nil {
		b.Fatal(err)
	}
	released := false
	defer func() {
		if !released {
			release()
		}
	}()
	profile, profileKey, err := fs.ReadAnyWithMasterHeld(ctx, p.Name, master)
	if err != nil {
		b.Fatal(err)
	}
	defer profileKey.Destroy()
	macKey, err := vault.ReceiptKey(profileKey, vid, profile.ID)
	if err != nil {
		b.Fatal(err)
	}
	defer macKey.Destroy()
	result, _ := json.Marshal(ChangeResult{Profile: p.Name, Revision: view.Revision})
	now := time.Now().Unix()
	files := map[string][]byte{}
	guard, err := platformfs.OpenRoot(root, false)
	if err != nil {
		b.Fatal(err)
	}
	defer guard.Close()
	if err := guard.MkdirAll("backup-receipts"); err != nil {
		b.Fatal(err)
	}
	for n := 1; n < size; n++ {
		id := automationMutationID(n)
		c := Change{Action: "secret.set", Key: "TOKEN", Value: &current, Revision: view.Revision, RequestID: id}
		fp, err := requestMAC(macKey, c)
		if err != nil {
			b.Fatal(err)
		}
		plain, _ := json.Marshal(vault.ValueReceipt{Fingerprint: fp, Result: result, ProfileID: profile.ID, KeyEpoch: profile.KeyEpoch, CreatedAt: now, ExpiresAt: now + int64(30*24*time.Hour/time.Second)})
		m := vault.NewMaterial(plain)
		clear(plain)
		body, err := vault.SealProtected(ctx, master, vid, valueReceiptScope(id), vault.MaterialValueReceipt, m)
		m.Destroy()
		if err != nil {
			b.Fatal(err)
		}
		// The initial corpus contains independent completed no-ops. Publish each
		// authenticated file durably, without staging redundant profile images.
		if err := guard.WriteFile(ctx, filepath.FromSlash(s.receiptRelative(id)), body, false); err != nil {
			b.Fatal(err)
		}
		clear(body)
	}
	if mode == "legacy_lookup" {
		path := s.receiptRelative(request.RequestID)
		raw, err := maintenance.Read(filepath.Join(root, path), vault.MaxDocumentBytes)
		if err != nil {
			b.Fatal(err)
		}
		plain, err := vault.OpenProtected(ctx, master, vid, valueReceiptScope(request.RequestID), vault.MaterialValueReceipt, raw)
		clear(raw)
		if err != nil {
			b.Fatal(err)
		}
		old, err := vault.SealProtected(ctx, master, vid, strings.ReplaceAll(request.RequestID, "-", ""), vault.MaterialValueReceipt, plain)
		plain.Destroy()
		if err != nil {
			b.Fatal(err)
		}
		files[path] = nil
		files[(Store{Directory: s.Directory, Profile: s.Profile}).receiptRelative(request.RequestID)] = old
	}
	if len(files) > 0 {
		if err := maintenance.Replace(root, files); err != nil {
			b.Fatal(err)
		}
	}
	release()
	released = true
	for _, n := range []int{1, size - 1} {
		c := Change{Action: "secret.set", Key: "TOKEN", Value: &current, Revision: view.Revision, RequestID: automationMutationID(n)}
		r, e := s.Apply(ctx, c)
		if e != nil || !r.Replayed || r.Changed {
			b.Fatal("invalid seeded no-op receipt", e)
		}
	}
	entries, err := os.ReadDir(filepath.Join(root, "backup-receipts"))
	if err != nil || len(entries) != size {
		b.Fatal("receipt population mismatch", len(entries), err)
	}
	return s, request, client, current
}

func benchmarkAutomationMutation(b *testing.B, mode string) {
	for _, size := range []int{1000, 10000} {
		b.Run(fmt.Sprintf("receipts_%d", size), func(b *testing.B) {
			s, request, client, want := automationMutationFixture(b, size, mode)
			ctx := context.Background()
			phases := map[string]time.Duration{}
			s.observe = func(phase string, d time.Duration) { phases[phase] += d }
			b.ReportAllocs()
			n := 0
			var totalHeld time.Duration
			for b.Loop() {
				b.StopTimer()
				c := request
				if mode == "fresh_edit" {
					view, e := s.Inspect(ctx, "")
					if e != nil {
						b.Fatal(e)
					}
					value := fmt.Sprintf("after-%d", n)
					c = Change{Action: "secret.set", Key: "TOKEN", Value: &value, Revision: view.Revision, RequestID: automationMutationID(size + n + 1)}
					want = value
				}
				clear(phases)
				runtime.GC()
				var before, after runtime.MemStats
				runtime.ReadMemStats(&before)
				b.StartTimer()
				start := time.Now()
				switch mode {
				case "fresh_edit":
					r, e := s.Apply(ctx, c)
					if e != nil || !r.Changed || r.Replayed {
						b.Fatal("fresh edit", e)
					}
				case "replay":
					r, e := s.Apply(ctx, request)
					if e != nil || !r.Replayed || !r.Changed {
						b.Fatal("replay", e)
					}
				default:
					r, e := s.InspectRequest(ctx, request.RequestID)
					if e != nil || r.Item.State != "completed" || r.Item.Result == nil || !r.Item.Result.Changed {
						b.Fatal("lookup", e)
					}
				}
				elapsed := time.Since(start)
				b.StopTimer()
				runtime.ReadMemStats(&after)
				held := phases["commit-held"]
				totalHeld += held
				sample, _ := json.Marshal(map[string]any{"sample": n, "metrics": map[string]any{"ns/op": elapsed.Nanoseconds(), "B/op": after.TotalAlloc - before.TotalAlloc, "allocs/op": after.Mallocs - before.Mallocs, "maintenance-held-ns/op": held.Nanoseconds()}})
				b.Logf("AUTOMATION_SAMPLE %s", sample)
				if mode == "fresh_edit" {
					if e := maintenance.Replace(s.root(), map[string][]byte{s.receiptRelative(c.RequestID): nil}); e != nil {
						b.Fatal(e)
					}
				}
				n++
				b.StartTimer() // Loop requires a running timer at its next call.
			}
			b.StopTimer()
			b.ReportMetric(float64(totalHeld.Nanoseconds())/float64(n), "maintenance-held-ns/op")
			master, err := client.InspectCached(ctx)
			if err != nil {
				b.Fatal(err)
			}
			defer master.Destroy()
			slot, err := (vault.KeyFileStore{Directory: filepath.Join(s.root(), "vault")}).Read()
			if err != nil {
				b.Fatal(err)
			}
			p, key, err := (vault.FileStore{Root: s.root(), VaultID: slot.VaultID, CatalogKey: master}).ReadWithMaster(ctx, s.Profile, master)
			if err != nil {
				b.Fatal(err)
			}
			defer key.Destroy()
			env, _, err := p.Environment("", key, time.Now())
			if err != nil || env["TOKEN"] != want {
				b.Fatal("measured operation changed the wrong stored value", err)
			}
		})
	}
}
func BenchmarkAutomationFreshEdit(b *testing.B)    { benchmarkAutomationMutation(b, "fresh_edit") }
func BenchmarkAutomationReplay(b *testing.B)       { benchmarkAutomationMutation(b, "replay") }
func BenchmarkAutomationLookup(b *testing.B)       { benchmarkAutomationMutation(b, "lookup") }
func BenchmarkAutomationLegacyLookup(b *testing.B) { benchmarkAutomationMutation(b, "legacy_lookup") }
