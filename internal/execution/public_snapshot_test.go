package execution

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jinyongp/devtools/internal/project"
	"github.com/jinyongp/devtools/internal/protocol"
	"github.com/jinyongp/devtools/internal/values"
	"github.com/jinyongp/devtools/internal/vault"
)

func TestPublicFrozenInputChild(t *testing.T) {
	if os.Getenv("DEVTOOLS_PUBLIC_SNAPSHOT_CHILD") != "1" {
		return
	}
	if os.Getenv("PUBLIC_URL") != "https://before.invalid" {
		os.Exit(42)
	}
	fmt.Print("public-snapshot-ok")
	os.Exit(0)
}
func TestPublicInputActualSpawnNeedsNoSessionAndDrainsOutcome(t *testing.T) {
	ctx := context.Background()
	root := privateTempDir(t)
	store := values.ModernStore{Directory: filepath.Join(root, "profiles"), Profile: "app"}
	if _, e := store.Update(ctx, func(s *values.Editor) (bool, *protocol.Error) {
		return s.Set(values.Variable, "PUBLIC_URL", "", "https://before.invalid")
	}); e != nil {
		t.Fatal(e)
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	command := Command{Project: project.Context{Profile: "app", Root: root}, Directory: root, Exec: []string{exe, "-test.run=^TestPublicFrozenInputChild$"}, Inject: true}
	dependencies := Dependencies{VaultDirectory: filepath.Join(root, "vault"), ValueSnapshot: func(ctx context.Context, profile, env string, inject bool) (*values.Snapshot, *protocol.Error) {
		return values.CapturePublic(ctx, root, profile, env, inject, time.Now())
	}}
	prepare := func() Prepared {
		t.Helper()
		p, e := PrepareInput(ctx, command, dependencies, []string{"DEVTOOLS_PUBLIC_SNAPSHOT_CHILD=1"}, PlanOptions{})
		if e != nil {
			t.Fatal(e)
		}
		t.Cleanup(p.Release)
		return p
	}
	prepared := prepare()
	if _, e := store.Update(ctx, func(s *values.Editor) (bool, *protocol.Error) {
		return s.Set(values.Variable, "PUBLIC_URL", "", "https://after.invalid")
	}); e != nil {
		t.Fatal(e)
	}
	var out, diagnostic bytes.Buffer
	code, e := prepared.Execute(ctx, nil, nil, nil, &out, &diagnostic)
	if e != nil || code != 0 || !strings.Contains(out.String(), "public-snapshot-ok") {
		t.Fatal("public actual spawn", code, e)
	}
	if err = vault.DrainLaunches(ctx, dependencies.VaultDirectory); err != nil {
		t.Fatal("public spawn outcome stayed unknown", err)
	}
	stale := prepare()
	id, _ := vault.NewID()
	pass := vault.NewMaterial([]byte("synthetic-public-to-protected"))
	defer pass.Destroy()
	master, err := (vault.KeyFileStore{Directory: dependencies.VaultDirectory}).Initialize(ctx, id, "passphrase", vault.PassphraseProtector{VaultID: id, Passphrase: pass})
	if err != nil {
		t.Fatal(err)
	}
	defer master.Destroy()
	out.Reset()
	diagnostic.Reset()
	if _, e = stale.Execute(ctx, nil, nil, nil, &out, &diagnostic); e == nil || e.Code != "input_changed" || out.Len() != 0 {
		t.Fatal("old unsigned input spawned after root setup", e)
	}
}
