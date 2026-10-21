package backup

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jinyongp/devtools/internal/protocol"
	"github.com/jinyongp/devtools/internal/values"
)

func TestPortableSnapshotExcludesHostNetworkState(t *testing.T) {
	e := Engine{Data: privateTestRoot(t)}
	if err := os.Chmod(e.Data, 0700); err != nil {
		t.Fatal(err)
	}
	store := values.Store{Directory: filepath.Join(e.Data, "profiles"), Profile: "app"}
	if _, err := store.Update(context.Background(), func(s *values.State) (bool, *protocol.Error) {
		return s.Set(values.Variable, "PUBLIC", "", "portable")
	}); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"ca", "exposure-auth", "exposures", "exposure-providers", "networkops", "hosts", "netsetup", "ports", "proxy", "value-executions", "value-execution-payloads", "value-execution-requests", "value-execution-locks", "value-runners", "value-runner-requests", "value-runner-transports"} {
		dir := filepath.Join(e.Data, name)
		if err := fixtureMkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
		if err := fixtureWriteFile(filepath.Join(dir, "private.json"), []byte("NETWORK_PRIVATE_CANARY"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	snapshot, err := e.snapshot("app")
	if err != nil {
		t.Fatal(err)
	}
	b, err := json.Marshal(snapshot)
	if err != nil || strings.Contains(string(b), "NETWORK_PRIVATE_CANARY") || len(snapshot.Profiles) != 1 {
		t.Fatal("portable snapshot included host state", err)
	}
}
