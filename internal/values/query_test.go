package values

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/jinyongp/devtools/internal/maintenance"
	"github.com/jinyongp/devtools/internal/protocol"
	"github.com/jinyongp/devtools/internal/storageerr"
	"github.com/jinyongp/devtools/internal/vault"
)

func TestQueryReadsExistingMetadataWithoutUsingMaster(t *testing.T) {
	for _, kind := range []string{"public", "protected", "legacy"} {
		t.Run(kind, func(t *testing.T) {
			store := ModernStore{Directory: filepath.Join(privateTempDir(t), "profiles"), Profile: "app"}
			if kind == "protected" {
				root, _, _, _ := vaultSnapshotFixture(t, 1)
				store.Directory = filepath.Join(root, "profiles")
			} else if kind == "legacy" {
				if err := maintenance.Write(filepath.Join(store.Directory, "617070.json"), []byte(`{"version":1,"profile":"app","envs":{},"keys":{}}`)); err != nil {
					t.Fatal(err)
				}
			} else {
				if _, err := store.Update(context.Background(), func(editor *Editor) (bool, *protocol.Error) {
					return editor.Set(Variable, "PUBLIC", "", "public")
				}); err != nil {
					t.Fatal(err)
				}
			}
			store.Master = func(context.Context) (*vault.Material, error) {
				t.Fatal("metadata query opened or extended key session")
				return nil, vault.ErrLocked
			}
			old, err := store.Read(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			query, err := store.ReadForQuery(context.Background())
			if err != nil || !reflect.DeepEqual(old, query) {
				t.Fatal("query changed public/metadata view", err)
			}
			encoded, marshalErr := json.Marshal(query)
			if marshalErr != nil || strings.Contains(string(encoded), "synthetic-slot-value") {
				t.Fatal("secret reached query view", marshalErr)
			}
		})
	}
}

func TestQueryPreparationErrorPreservesStorageAndContextContract(t *testing.T) {
	store := ModernStore{Profile: "app"}
	for _, state := range []string{"initialization_required", "recovery_required"} {
		cause := storageerr.Wrap("open_file", "private_file", "access_denied", &os.PathError{
			Op: "open", Path: "/synthetic-private-path", Err: os.ErrPermission,
		})
		failure := store.queryError(context.Background(), &maintenance.QueryPreparationError{State: state, Cause: cause})
		public := protocol.Public(failure, "variable list")
		facts := public.Details["facts"].(map[string]any)
		if failure.Code != "storage_error" || failure.ExitCode != 3 || facts["stage"] != "open_file" || facts["state"] != state || public.Details["reason"] != "access_denied" {
			t.Fatal("preparation lost original failure or state", public)
		}
		if public.Details["effects"] != "unknown" || public.Details["retry"] != "after_dependency_change" {
			t.Fatal("failed writable recovery claimed no effects")
		}
		remedies := public.Details["remedies"].([]protocol.Remedy)
		if len(remedies) != 1 || remedies[0].Effect != "value_write" || !reflect.DeepEqual(remedies[0].Argv, []string{"devtools", "variable", "list", "--profile", "app"}) {
			t.Fatal("unsafe or unusable recovery remedy")
		}
		encoded, _ := json.Marshal(public)
		if strings.Contains(string(encoded), "synthetic-private-path") {
			t.Fatal("query error exposed private path")
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	failure := store.queryError(ctx, &maintenance.QueryPreparationError{State: "recovery_required", Cause: os.ErrPermission})
	if failure.Code != "canceled" {
		t.Fatal("preparation overrode caller cancellation")
	}
}

func TestQueryRejectsInvalidProfileBeforeStoragePreparation(t *testing.T) {
	root := filepath.Join(privateTempDir(t), "absent")
	store := ModernStore{Directory: filepath.Join(root, "profiles"), Profile: "../invalid"}
	if _, err := store.ReadForQuery(context.Background()); err == nil || err.Code != "invalid_argument" {
		t.Fatal(err)
	}
	if _, err := os.Stat(root); !os.IsNotExist(err) {
		t.Fatal("invalid query initialized storage")
	}
}
