package maintenance

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
)

func TestLegacyRecoveryFenceSurvivesRollbackAndCommit(t *testing.T) {
	for _, vaultID := range []string{"", strings.Repeat("a", 32)} {
		t.Run("vault="+vaultID, func(t *testing.T) {
			ctx := context.Background()
			root := privateTempDir(t)
			release, err := AcquireExclusive(ctx, root)
			if err != nil {
				t.Fatal(err)
			}
			if vaultID != "" {
				if err := writeProtectedRoot(root, vaultID); err != nil {
					t.Fatal(err)
				}
			}
			if err := FenceLegacyRecoveryHeld(root); err != nil {
				t.Fatal(err)
			}
			if err := FenceLegacyRecoveryHeld(root); err != nil {
				t.Fatal(err)
			}
			ensureFence := func() {
				t.Helper()
				marker, err := Read(filepath.Join(root, legacyRecoveryPointer), 4096)
				if err != nil || string(marker) != legacyRecoveryFence {
					t.Fatal("legacy recovery path reopened", err)
				}
			}
			ensureFence()
			path := "tasks/61.json"
			if err := Write(filepath.Join(root, path), []byte("before")); err != nil {
				t.Fatal(err)
			}
			files := map[string][]byte{path: []byte("after")}
			if _, err := prepareTransaction(root, files); err != nil {
				t.Fatal(err)
			}
			if err := applyReplacement(root, files); err != nil {
				t.Fatal(err)
			}
			ensureFence()
			release()
			release, err = AcquireExclusive(ctx, root)
			if err != nil {
				t.Fatal(err)
			}
			defer release()
			ensureFence()
			if body, err := Read(filepath.Join(root, path), 1024); err != nil || string(body) != "before" {
				t.Fatal("rollback lost the old content", err)
			}
			if err := Replace(root, files); err != nil {
				t.Fatal(err)
			}
			ensureFence()
			if body, err := Read(filepath.Join(root, path), 1024); err != nil || string(body) != "after" {
				t.Fatal("commit lost the new content", err)
			}
			if id, protected, err := ProtectedRootHeld(root); err != nil || id != vaultID || protected != (vaultID != "") {
				t.Fatal("fenced recovery changed the protected-root identity", err)
			}
		})
	}
}
