package values

import (
	"context"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"

	"github.com/jinyongp/devtools/internal/platformfs"
	"github.com/jinyongp/devtools/internal/protocol"
)

func TestOrdinaryOperationsRequireExplicitLegacyUpgrade(t *testing.T) {
	for _, operation := range []string{"read", "inspect", "write"} {
		t.Run(operation, func(t *testing.T) {
			ctx := context.Background()
			s := ModernStore{Directory: filepath.Join(privateTempDir(t), "profiles"), Profile: "LegacyProfile"}
			if err := platformfs.MkdirPrivate(s.Directory); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(s.Directory, hex.EncodeToString([]byte(s.Profile))+".json")
			source := []byte(`{"version":1,"profile":"LegacyProfile","envs":{},"keys":{"URL":{"kind":"variable","common":"before","envs":{}}}}`)
			if err := writePrivateFixture(path, source, 0600); err != nil {
				t.Fatal(err)
			}
			var failure *protocol.Error
			switch operation {
			case "read":
				_, failure = s.ReadForQuery(ctx)
			case "inspect":
				_, failure = s.Inspect(ctx, "")
			case "write":
				_, failure = s.Update(ctx, func(*Editor) (bool, *protocol.Error) { t.Fatal("legacy edit callback ran"); return false, nil })
			}
			if failure == nil || failure.Code != "migration_required" {
				t.Fatal(failure)
			}
			got, err := os.ReadFile(path)
			if err != nil || string(got) != string(source) {
				t.Fatal("ordinary operation converted legacy data", err)
			}
		})
	}
}
