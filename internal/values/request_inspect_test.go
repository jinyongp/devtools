package values

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jinyongp/devtools/internal/maintenance"
	"github.com/jinyongp/devtools/internal/profilekey"
	"github.com/jinyongp/devtools/internal/protocol"
	"github.com/jinyongp/devtools/internal/vault"
)

func TestManagedRequestHistoricalEncoding(t *testing.T) {
	value := "example"
	c := Change{Action: "variable.set", Key: "URL", Value: &value, Revision: "old-revision", RequestID: "ABCDEF00-0000-4000-8000-000000000001"}
	const original = `{"action":"variable.set","key":"URL","env":"","value":"example","revision":"old-revision","request_id":"ABCDEF00-0000-4000-8000-000000000001"}`
	raw, err := json.Marshal(c)
	if err != nil || string(raw) != original {
		t.Fatal("absent new options changed historical request bytes", err)
	}
	key := vault.NewMaterial(bytes.Repeat([]byte{0x41}, 32))
	defer key.Destroy()
	want, err := requestMAC(key, c)
	if err != nil || want != "a31a5bbd2b97617af81f2fde1e86bced87698adf325b3f45f6234281b97c50a2" {
		t.Fatal(err)
	}
	c.CreateOnly = true
	other, err := requestMAC(key, c)
	if err != nil || other == want {
		t.Fatal("create-only was not bound to request identity", err)
	}
	c.CreateOnly = false
	c.Generate = &GenerateChange{}
	other, err = requestMAC(key, c)
	if err != nil || other == want {
		t.Fatal("generation was not bound to request identity", err)
	}
}

func TestManagedCreateOnlyExactSlots(t *testing.T) {
	ctx := context.Background()
	root, files, master, profileKey := vaultSnapshotFixture(t, 0)
	var calls atomic.Int32
	store := ModernStore{Directory: filepath.Join(root, "profiles"), Profile: "app", Master: snapshotMaster(master, &calls)}
	for _, kind := range []Kind{Variable, Secret} {
		t.Run(string(kind), func(t *testing.T) {
			key := strings.ToUpper(string(kind))
			apply := func(env, value string) (ChangeResult, *protocol.Error) {
				t.Helper()
				view, e := store.Inspect(ctx, env)
				if e != nil {
					t.Fatal(e)
				}
				return store.Apply(ctx, Change{Action: string(kind) + ".set", Key: key, Env: env, Value: &value, CreateOnly: true, Revision: view.Revision, RequestID: testRequestID(t)})
			}
			if _, e := apply("", ""); e != nil {
				t.Fatal("empty slot creation", e)
			}
			if _, e := apply("", "replacement"); e == nil || e.Code != "value_exists" {
				t.Fatal("empty slot treated as absent", e)
			}
			if _, e := apply("local", "override"); e != nil {
				t.Fatal("inherited common counted as local slot", e)
			}
			if _, e := apply("local", "replacement"); e == nil || e.Code != "value_exists" {
				t.Fatal("existing override replaced", e)
			}
			// Expired slots retain their identity and still block creation.
			if _, err := files.Update(ctx, "app", profileKey, func(p *vault.Profile) (bool, error) {
				for i := range p.Values {
					if p.Values[i].Name == key {
						expired := time.Now().Add(-time.Minute).Unix()
						for j := range p.Values[i].Slots {
							p.Values[i].Slots[j].ExpiresAt = &expired
						}
					}
				}
				p.Revision++
				return true, nil
			}); err != nil {
				t.Fatal(err)
			}
			for _, env := range []string{"", "local"} {
				if _, e := apply(env, "replacement"); e == nil || e.Code != "value_exists" {
					t.Fatal("expired slot treated as absent", e)
				}
			}
		})
	}
}

func testRequestID(t *testing.T) string {
	t.Helper()
	id, err := vault.NewID()
	if err != nil {
		t.Fatal(err)
	}
	return id[:8] + "-" + id[8:12] + "-" + id[12:16] + "-" + id[16:20] + "-" + id[20:]
}

func TestManagedRequestLookupPublicRetentionAndNoPublication(t *testing.T) {
	ctx := context.Background()
	root := privateTempDir(t)
	store := ModernStore{Directory: filepath.Join(root, "profiles"), Profile: "app"}
	id := "ABCDEF00-0000-4000-8000-000000000001"
	if _, e := store.InspectRequest(ctx, id); e == nil || e.Code != "request_not_found" {
		t.Fatal("missing lookup", e)
	}
	keyPath := filepath.Join(root, ".maintenance", "values-key")
	if _, err := os.Stat(keyPath); !os.IsNotExist(err) {
		t.Fatal("missing lookup created verification key")
	}
	view, e := store.Inspect(ctx, "")
	if e != nil {
		t.Fatal(e)
	}
	value := "synthetic-public-value"
	c := Change{Action: "variable.set", Key: "URL", Value: &value, Revision: view.Revision, RequestID: id, CreateOnly: true}
	saved, e := store.Apply(ctx, c)
	if e != nil {
		t.Fatal(e)
	}
	path := filepath.Join(store.Directory, profilekey.CanonicalFilename("app"))
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	receiptPath := (Store{Directory: store.Directory, Profile: store.Profile}).receiptRelative(id)
	receiptBefore, err := os.ReadFile(filepath.Join(root, receiptPath))
	if err != nil {
		t.Fatal(err)
	}
	// A pending rollback can restore a receipt that was absent on disk. Lookup
	// must recover first instead of interpreting that intermediate state as lost.
	release, err := maintenance.AcquireExclusive(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	marker, _ := json.Marshal([]struct {
		Path   string `json:"path"`
		Data   []byte `json:"data"`
		Exists bool   `json:"exists"`
	}{{receiptPath, receiptBefore, true}})
	err = maintenance.Write(filepath.Join(root, ".maintenance", "restore-pending.json"), marker)
	if err == nil {
		err = os.Remove(filepath.Join(root, receiptPath))
	}
	release()
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		got, e := store.InspectRequest(ctx, id)
		if e != nil || got.Item.State != "completed" || got.Item.Result == nil || !reflect.DeepEqual(*got.Item.Result, saved) || got.Item.CreatedAt == nil || got.Item.ExpiresAt.Sub(*got.Item.CreatedAt) != 30*24*time.Hour {
			t.Fatal("saved metadata lookup", e)
		}
	}
	if _, e := store.InspectRequest(ctx, strings.ToLower(id)); e == nil || e.Code != "request_not_found" {
		t.Fatal("request UUID was silently case normalized", e)
	}
	if replay, e := store.Apply(ctx, c); e != nil || !replay.Replayed || !replay.Changed || replay.Revision != saved.Revision {
		t.Fatal("create-only replay rejected its own slot", e)
	}
	after, _ := os.ReadFile(path)
	receiptAfter, _ := os.ReadFile(filepath.Join(root, receiptPath))
	if !bytes.Equal(before, after) || !bytes.Equal(receiptBefore, receiptAfter) {
		t.Fatal("lookup or replay published new state")
	}
	for _, expired := range []bool{true, false} {
		var receipt vault.ValueReceipt
		if err := json.Unmarshal(receiptBefore, &receipt); err != nil {
			t.Fatal(err)
		}
		if expired {
			receipt.CreatedAt = time.Now().Add(-time.Hour).Unix()
			receipt.ExpiresAt = time.Now().Unix()
		} else {
			receipt.CreatedAt, receipt.ExpiresAt = 0, 0
		}
		raw, _ := json.Marshal(receipt)
		release, err := maintenance.AcquireExclusive(ctx, root)
		if err != nil {
			t.Fatal(err)
		}
		err = maintenance.Replace(root, map[string][]byte{receiptPath: raw})
		release()
		if err != nil {
			t.Fatal(err)
		}
		got, e := store.InspectRequest(ctx, id)
		if e != nil {
			t.Fatal(e)
		}
		if expired {
			if got.Item.State != "expired" || got.Item.Result != nil {
				t.Fatal("expired lookup exposed completed result")
			}
			if _, e := store.Apply(ctx, c); e == nil || e.Code != "request_expired" {
				t.Fatal("expired request executed again", e)
			}
		} else if got.Item.State != "completed" || got.Item.Result == nil || got.Item.CreatedAt != nil || got.Item.ExpiresAt != nil {
			t.Fatal("legacy unlimited request changed")
		} else if replay, e := store.Apply(ctx, c); e != nil || !replay.Replayed {
			t.Fatal("legacy request lost unlimited replay", e)
		}
	}
}

func TestManagedRequestLookupRejectsConflictingReceiptExpiry(t *testing.T) {
	ctx := context.Background()
	store := ModernStore{Directory: filepath.Join(privateTempDir(t), "profiles"), Profile: "app"}
	view, e := store.Inspect(ctx, "")
	if e != nil {
		t.Fatal(e)
	}
	value := "synthetic-public-value"
	id := testRequestID(t)
	c := Change{Action: "variable.set", Key: "URL", Value: &value, Revision: view.Revision, RequestID: id}
	if _, e := store.Apply(ctx, c); e != nil {
		t.Fatal(e)
	}
	canonical := (Store{Directory: store.Directory, Profile: store.Profile}).receiptRelative(id)
	raw, err := maintenance.Read(filepath.Join(store.root(), canonical), vault.MaxDocumentBytes)
	if err != nil {
		t.Fatal(err)
	}
	var receipt vault.ValueReceipt
	if err := json.Unmarshal(raw, &receipt); err != nil {
		t.Fatal(err)
	}
	receipt.ExpiresAt++
	raw, _ = json.Marshal(receipt)
	legacy := "backup-receipts/values-" + hex.EncodeToString([]byte(store.Profile)) + "-" + id + ".json"
	release, err := maintenance.AcquireExclusive(ctx, store.root())
	if err != nil {
		t.Fatal(err)
	}
	err = maintenance.Replace(store.root(), map[string][]byte{legacy: raw})
	release()
	if err != nil {
		t.Fatal(err)
	}
	if _, e := store.InspectRequest(ctx, id); e == nil || e.Code != "integrity_failed" {
		t.Fatal("conflicting copies changed lookup expiry", e)
	}
	if _, e := store.Apply(ctx, c); e == nil || e.Code != "integrity_failed" {
		t.Fatal("conflicting copies changed replay expiry", e)
	}
}

func TestManagedCreateOnlyConcurrentCommitAndLaterWriter(t *testing.T) {
	ctx := context.Background()
	s := ModernStore{Directory: filepath.Join(privateTempDir(t), "profiles"), Profile: "app"}
	view, e := s.Inspect(ctx, "")
	if e != nil {
		t.Fatal(e)
	}
	value := "synthetic-original"
	c := Change{Action: "variable.set", Key: "URL", Value: &value, Revision: view.Revision, RequestID: testRequestID(t), CreateOnly: true}
	paused, ready, resume := pauseCommit(t, s)
	type outcome struct {
		result ChangeResult
		err    *protocol.Error
	}
	done := make(chan outcome, 1)
	go func() { r, e := paused.Apply(ctx, c); done <- outcome{r, e} }()
	awaitPrepared(t, ready)
	first, e := s.Apply(ctx, c)
	if e != nil {
		t.Fatal(e)
	}
	resume()
	last := <-done
	if last.err != nil || !last.result.Replayed || last.result.Revision != first.Revision || last.result.Changed != first.Changed {
		t.Fatal("concurrent create-only did not replay", last.err)
	}
	if _, e := setPublic(ctx, s, "synthetic-later"); e != nil {
		t.Fatal(e)
	}
	if replay, e := s.Apply(ctx, c); e != nil || !replay.Replayed || replay.Revision != first.Revision {
		t.Fatal("later edit broke original replay", e)
	}
	state, e := s.Read(ctx)
	if e != nil {
		t.Fatal(e)
	}
	if got, _, e := state.GetVariable("URL", ""); e != nil || got != "synthetic-later" {
		t.Fatal("replay overwrote later writer", e)
	}
}

func TestManagedRequestLookupRecoveryFailureBeforeNotFound(t *testing.T) {
	ctx := context.Background()
	root := privateTempDir(t)
	release, err := maintenance.AcquireExclusive(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	err = maintenance.Write(filepath.Join(root, ".maintenance", "restore-pending.json"), []byte(`{"unsupported_pending":true}`))
	release()
	if err != nil {
		t.Fatal(err)
	}
	store := ModernStore{Directory: filepath.Join(root, "profiles"), Profile: "app"}
	if _, e := store.InspectRequest(ctx, testRequestID(t)); e == nil || e.Code == "request_not_found" {
		t.Fatal("unrecovered storage reported request absent", e)
	}
}

func TestManagedRequestLookupProtectedUsesOnlyWarmSession(t *testing.T) {
	root, files, master, _ := vaultSnapshotFixture(t, 0)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	var calls atomic.Int32
	store := ModernStore{Directory: filepath.Join(root, "profiles"), Profile: "app", Master: snapshotMaster(master, &calls)}
	view, e := store.Inspect(ctx, "")
	if e != nil {
		t.Fatal(e)
	}
	value := "synthetic-request-private-canary"
	id := testRequestID(t)
	saved, e := store.Apply(ctx, Change{Action: "secret.set", Key: "TOKEN", Value: &value, Revision: view.Revision, RequestID: id})
	if e != nil {
		t.Fatal(e)
	}
	// Neither callback may be invoked by a passive lookup.
	store.Master = func(context.Context) (*vault.Material, error) {
		t.Error("lookup called active key provider")
		return nil, vault.ErrLocked
	}
	store.WriteMaster = func(context.Context) (*vault.Material, func(context.Context) error, error) {
		t.Error("lookup called write key provider")
		return nil, nil, vault.ErrLocked
	}
	dir := filepath.Join(root, "vault")
	if _, e := store.InspectRequest(ctx, id); e == nil {
		t.Fatal("cold lookup succeeded")
	}
	if _, err := os.Stat(filepath.Join(dir, "session.json")); !os.IsNotExist(err) {
		t.Fatal("cold lookup started daemon")
	}
	finished := make(chan error, 1)
	go func() { finished <- vault.ServeKeySession(ctx, dir) }()
	defer func() {
		cancel()
		select {
		case err := <-finished:
			if err != nil {
				t.Error(err)
			}
		case <-time.After(3 * time.Second):
			t.Error("owned fixture did not stop")
		}
	}()
	var client *vault.SessionClient
	for {
		var err error
		client, err = vault.OpenSessionClient(dir)
		if err == nil {
			if _, err = client.Status(ctx); err == nil {
				break
			}
		}
		select {
		case <-ctx.Done():
			t.Fatal("session fixture not ready")
		case <-time.After(10 * time.Millisecond):
		}
	}
	if _, e := store.InspectRequest(ctx, id); e == nil || e.Code != "vault_locked" {
		t.Fatal("cold daemon lookup unlocked", e)
	}
	pass := vault.NewMaterial([]byte("synthetic-snapshot-unlock-only"))
	defer pass.Destroy()
	if err := client.Unlock(ctx, pass, false); err != nil {
		t.Fatal(err)
	}
	before, err := client.Status(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		got, e := store.InspectRequest(ctx, id)
		if e != nil || got.Item.Result == nil || !reflect.DeepEqual(*got.Item.Result, saved) {
			t.Fatal("warm lookup", e)
		}
		raw, err := json.Marshal(got)
		if err != nil || bytes.Contains(raw, []byte(value)) {
			t.Fatal("lookup leaked input", err)
		}
	}
	after, err := client.Status(ctx)
	if err != nil || before.IdleExpiresAt == nil || after.IdleExpiresAt == nil || !before.IdleExpiresAt.Equal(*after.IdleExpiresAt) {
		t.Fatal("lookup extended idle", err)
	}
	if _, err := files.RotateProfile(ctx, "app", master); err != nil {
		t.Fatal(err)
	}
	if got, e := store.InspectRequest(ctx, id); e != nil || got.Item.Result == nil || !reflect.DeepEqual(*got.Item.Result, saved) {
		t.Fatal("rotation lost metadata lookup", e)
	}
	if err := client.Lock(ctx); err != nil {
		t.Fatal(err)
	}
	for _, request := range []string{id, testRequestID(t)} {
		if _, e := store.InspectRequest(ctx, request); e == nil || e.Code != "vault_locked" {
			t.Fatal("locked lookup used or disclosed receipt", e)
		}
	}
}

func TestManagedCreateOnlyInvalidOptionsAndFailedRequestsDoNotBind(t *testing.T) {
	ctx := context.Background()
	store := ModernStore{Directory: filepath.Join(privateTempDir(t), "profiles"), Profile: "app"}
	view, e := store.Inspect(ctx, "")
	if e != nil {
		t.Fatal(e)
	}
	value := "synthetic-value"
	for _, c := range []Change{
		{Action: "env.create", Env: "local", CreateOnly: true},
		{Action: "variable.unset", Key: "URL", CreateOnly: true},
		{Action: "variable.set", Key: "URL", Value: &value, Generate: &GenerateChange{}},
		{Action: "bundle.set", Bundle: &BundleChange{Name: "test"}, CreateOnly: true},
		{Action: "secret.generate", Generate: &GenerateChange{}, Value: &value},
	} {
		c.RequestID, c.Revision = testRequestID(t), view.Revision
		if _, e := store.Apply(ctx, c); e == nil || e.Code != "invalid_argument" {
			t.Fatal("invalid option combination accepted", e)
		}
		if _, e := store.InspectRequest(ctx, c.RequestID); e == nil || e.Code != "request_not_found" {
			t.Fatal("failed request consumed ID", e)
		}
	}
}
