package valuehandoff

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/jinyongp/devtools/internal/maintenance"
	"github.com/jinyongp/devtools/internal/platformfs"
	"github.com/jinyongp/devtools/internal/protocol"
	"github.com/jinyongp/devtools/internal/values"
	"github.com/jinyongp/devtools/internal/vault"
)

func TestMain(m *testing.M) {
	if len(os.Args) == 3 && os.Args[1] == "__vault-session" {
		if vault.ServeKeySession(context.Background(), os.Args[2]) != nil {
			os.Exit(91)
		}
		os.Exit(0)
	}
	os.Exit(m.Run())
}
func fixture(t *testing.T) (Store, *vault.SessionClient) {
	t.Helper()
	ctx := context.Background()
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(base, "private")
	guard, err := platformfs.OpenRoot(root, true)
	if err != nil {
		t.Fatal(err)
	}
	guard.Close()
	id, _ := vault.NewID()
	pass := vault.NewMaterial([]byte("synthetic-handoff-passphrase"))
	defer pass.Destroy()
	ks := vault.KeyFileStore{Directory: filepath.Join(root, "vault")}
	master, err := ks.Initialize(ctx, id, "passphrase", vault.PassphraseProtector{VaultID: id, Passphrase: pass})
	if err != nil {
		t.Fatal(err)
	}
	defer master.Destroy()
	key, err := vault.NewKey()
	if err != nil {
		t.Fatal(err)
	}
	defer key.Destroy()
	p, err := vault.NewSealedProfile("app", id, master, key)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"TOKEN", "OTHER"} {
		if _, err = p.Set(name, vault.Secret, "", "synthetic-handoff-canary-"+name, key); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = p.SetBundle("api", []string{"TOKEN"}, key); err != nil {
		t.Fatal(err)
	}
	files := vault.FileStore{Root: root, VaultID: id, CatalogKey: master}
	if err = files.Initialize(ctx); err != nil {
		t.Fatal(err)
	}
	if err = files.Create(ctx, p, key); err != nil {
		t.Fatal(err)
	}
	client, err := (vault.SessionManager{Directory: ks.Directory}).Client(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := client.Retire(ctx); err != nil {
			t.Error(err)
		}
	})
	if err = client.Unlock(ctx, pass, false); err != nil {
		t.Fatal(err)
	}
	return Store{root, "app"}, client
}
func requestID(n int) string { return fmt.Sprintf("00000000-0000-4000-8000-%012x", n) }
func TestPrepareImmutableReplayPassiveLookupAndPrivacy(t *testing.T) {
	s, c := fixture(t)
	ctx := context.Background()
	input := PrepareInput{ExternalExecutionID: "job-1", Selection: Selection{Keys: []string{"TOKEN"}, Bundle: "api"}}
	r, e := s.Prepare(ctx, input, requestID(1))
	if e != nil {
		t.Fatal(e)
	}
	if r.Item.Revision != 1 || r.Item.DeliveryState != "prepared" || r.Item.ExecutionState != "not_started" || !r.Changed || r.Replayed {
		t.Fatal("wrong initial metadata")
	}
	if r.Item.ExpiresAt.Sub(r.Item.CreatedAt) > 30*time.Second {
		t.Fatal("deadline extended")
	}
	store := values.CurrentStore(filepath.Join(s.Root, "profiles"), s.Profile)
	if _, e := store.Update(ctx, func(ed *values.Editor) (bool, *protocol.Error) {
		return ed.Set(values.Secret, "TOKEN", "", "later-writer")
	}); e != nil {
		t.Fatal(e)
	}
	r2, e := s.Prepare(ctx, input, requestID(1))
	if e != nil || !r2.Replayed || r2.Item != r.Item {
		t.Fatal("lost-response replay changed original input", e)
	}
	input.Selection.Keys = append(input.Selection.Keys, "OTHER")
	if _, e = s.Prepare(ctx, input, requestID(1)); e == nil || e.Code != "request_conflict" {
		t.Fatal("changed input reused request", e)
	}
	a, err := s.access(ctx, false)
	if err != nil {
		t.Fatal(err)
	}
	defer a.master.Destroy()
	var snap struct {
		Values []storedValue `json:"values"`
	}
	gate, err := maintenance.AcquireShared(ctx, s.Root)
	if err != nil {
		t.Fatal(err)
	}
	err = s.decode(ctx, a, s.path("value-execution-payloads", r.Item.ID), r.Item.ID, vault.MaterialExecutionSnapshot, &snap)
	gate()
	if err != nil || len(snap.Values) != 1 || snap.Values[0].Value != "synthetic-handoff-canary-TOKEN" {
		t.Fatal("frozen bytes changed", err)
	}
	before, err := c.Status(ctx)
	if err != nil {
		t.Fatal(err)
	}
	v, e := s.Status(ctx, "", requestID(1))
	if e != nil || v.Item.ID != r.Item.ID {
		t.Fatal("prepare lookup failed", e)
	}
	page, e := s.List(ctx, 50, "")
	if e != nil || len(page.Items) != 1 {
		t.Fatal("list failed", e)
	}
	after, err := c.Status(ctx)
	if err != nil {
		t.Fatal(err)
	}
	b1, _ := json.Marshal(before)
	b2, _ := json.Marshal(after)
	if !bytes.Equal(b1, b2) {
		t.Fatal("passive metadata changed session status")
	}
	for _, public := range []any{r, v, page} {
		b, _ := json.Marshal(public)
		if bytes.Contains(b, []byte("canary")) || bytes.Contains(b, []byte("ticket")) {
			t.Fatal("private input in public response")
		}
	}
	if _, err = json.Marshal(snapshot{Values: snap.Values}); err == nil {
		t.Fatal("snapshot serialization allowed")
	}
	if fmt.Sprintf("%+v", snapshot{Values: snap.Values}) != "[REDACTED]" {
		t.Fatal("snapshot fmt exposed values")
	}
	err = filepath.WalkDir(s.Root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		// OS coordination locks never store value bytes. Windows byte-range
		// locks intentionally deny reads while the owned daemon is running.
		if !d.Type().IsRegular() || filepath.Ext(path) == ".lock" {
			return nil
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if bytes.Contains(b, []byte("synthetic-handoff-canary")) {
			t.Fatal("plaintext persisted")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if err = c.Lock(ctx); err != nil {
		t.Fatal(err)
	}
	if _, e = s.Status(ctx, r.Item.ID, ""); e == nil {
		t.Fatal("locked metadata read succeeded")
	}
}
func TestPrepareScopeAndEmptySelection(t *testing.T) {
	s, _ := fixture(t)
	ctx := context.Background()
	cases := []struct {
		selection Selection
		code      string
	}{{Selection{Keys: nil}, "invalid_argument"}, {Selection{Keys: []string{"TOKEN", "TOKEN"}}, "invalid_argument"}, {Selection{Keys: []string{"MISSING"}}, "key_not_found"}, {Selection{Keys: []string{}, Bundle: "api"}, "selection_outside_scope"}, {Selection{Keys: []string{"TOKEN"}, Bundle: "missing"}, "bundle_not_found"}}
	for i, tc := range cases {
		_, e := s.Prepare(ctx, PrepareInput{ExternalExecutionID: "job", Selection: tc.selection}, requestID(i+10))
		if e == nil {
			t.Fatal("invalid selection accepted", i)
		}
		if e.Code != tc.code {
			t.Fatal(i, e)
		}
	}
	r, e := s.Prepare(ctx, PrepareInput{ExternalExecutionID: "empty", Selection: Selection{Keys: []string{}}}, requestID(20))
	if e != nil || r.Item.ID == "" {
		t.Fatal("explicit empty selection failed", e)
	}
}
func TestExecutionEnvelopeBindingAndCursor(t *testing.T) {
	s, _ := fixture(t)
	ctx := context.Background()
	var ids []string
	for i := 0; i < 3; i++ {
		r, e := s.Prepare(ctx, PrepareInput{ExternalExecutionID: fmt.Sprint("job-", i), Selection: Selection{Keys: []string{}}}, requestID(30+i))
		if e != nil {
			t.Fatal(e)
		}
		ids = append(ids, r.Item.ID)
	}
	page, e := s.List(ctx, 1, "")
	if e != nil || page.NextCursor == nil {
		t.Fatal("missing cursor", e)
	}
	page2, e := s.List(ctx, 1, *page.NextCursor)
	if e != nil || len(page2.Items) != 1 || page.Items[0].ID == page2.Items[0].ID {
		t.Fatal("pagination lost progress", e)
	}
	if _, e := s.Prepare(ctx, PrepareInput{ExternalExecutionID: "job-3", Selection: Selection{Keys: []string{}}}, requestID(33)); e != nil {
		t.Fatal(e)
	}
	if _, e = s.List(ctx, 1, *page.NextCursor); e == nil || e.Code != "cursor_invalid" {
		t.Fatal("stale cursor accepted", e)
	}
	gate, err := maintenance.AcquireExclusive(ctx, s.Root)
	if err != nil {
		t.Fatal(err)
	}
	first, err := maintenance.Read(filepath.Join(s.Root, s.path("value-executions", ids[0])), vault.MaxDocumentBytes)
	if err != nil {
		t.Fatal(err)
	}
	err = maintenance.Replace(s.Root, map[string][]byte{s.path("value-executions", ids[1]): first})
	gate()
	if err != nil {
		t.Fatal(err)
	}
	if _, e = s.Status(ctx, ids[1], ""); e == nil || e.Code != vault.ErrIntegrity.Error() {
		t.Fatal("swapped record accepted", e)
	}
}

func TestConcurrentPreparePublishesOneInputAndExpiredRequestStaysSpent(t *testing.T) {
	s, _ := fixture(t)
	ctx := context.Background()
	input := PrepareInput{ExternalExecutionID: "job-once", Selection: Selection{Keys: []string{"TOKEN"}}}
	var wg sync.WaitGroup
	results := make(chan Result, 4)
	for range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r, e := s.Prepare(ctx, input, requestID(100))
			if e != nil {
				t.Error(e)
				return
			}
			results <- r
		}()
	}
	wg.Wait()
	close(results)
	id := ""
	fresh := 0
	for r := range results {
		if id == "" {
			id = r.Item.ID
		}
		if id != r.Item.ID {
			t.Fatal("duplicate input committed")
		}
		if !r.Replayed {
			fresh++
		}
	}
	if fresh != 1 {
		t.Fatal("expected one fresh commit", fresh)
	}
	a, err := s.access(ctx, false)
	if err != nil {
		t.Fatal(err)
	}
	defer a.master.Destroy()
	gate, err := s.lockCommit(ctx, a)
	if err != nil {
		t.Fatal(err)
	}
	path := s.path("value-execution-requests", requestID(100))
	var rr receipt
	if err = s.decode(ctx, a, path, requestID(100), vault.MaterialExecutionRequest, &rr); err != nil {
		gate()
		t.Fatal(err)
	}
	rr.ExpiresAt = time.Now().Add(-time.Second)
	b, err := s.encode(ctx, a, path, requestID(100), vault.MaterialExecutionRequest, rr)
	if err == nil {
		err = maintenance.Replace(s.Root, map[string][]byte{path: b})
	}
	gate()
	if err != nil {
		t.Fatal(err)
	}
	if _, e := s.Prepare(ctx, input, requestID(100)); e == nil || e.Code != "request_expired" {
		t.Fatal("expired request executed", e)
	}
	page, e := s.List(ctx, 50, "")
	if e != nil || len(page.Items) != 1 {
		t.Fatal("expired ID created another execution", e)
	}
}
