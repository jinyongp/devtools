package maintenance

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestWindowsCommitPublicationBlockedRetainsRecovery(t *testing.T) {
	root := privateTempDir(t)
	release, err := AcquireExclusive(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(root, "profiles", "61.json")
	if err = Write(target, []byte("original")); err != nil {
		release()
		t.Fatal(err)
	}
	files := map[string][]byte{"profiles/61.json": []byte("replacement")}
	pointer, err := prepareTransaction(root, files)
	if err != nil {
		release()
		t.Fatal(err)
	}
	if err = applyReplacement(root, files); err != nil {
		release()
		t.Fatal(err)
	}
	// Windows os.Open deliberately denies delete sharing; the committed pointer
	// cannot be replaced while this external reader exists.
	reader, err := os.Open(pendingPath(root))
	if err != nil {
		release()
		t.Fatal(err)
	}
	committed, commitErr := markCommitted(root, pointer)
	_ = reader.Close()
	release()
	if committed || commitErr == nil {
		t.Fatal("failed WT publication became success")
	}
	release, err = AcquireExclusive(context.Background(), root)
	if err != nil {
		t.Fatal("recovery failed", err)
	}
	defer release()
	data, err := Read(target, 1<<20)
	if err != nil || string(data) != "original" {
		t.Fatal("applying transaction not rolled back", err)
	}
	if _, err = os.Stat(pendingPath(root)); !os.IsNotExist(err) {
		t.Fatal("recovery pointer remains", err)
	}
}
