package profilekey

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDirectIdentityReadRejectsSymlinkedDirectory(t *testing.T) {
	directory := privateTempDir(t)
	outside := privateTempDir(t)
	profile := "identity-probe"
	body, err := IdentityBytes(profile, "values")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(outside, CanonicalFilename(profile)), body, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(directory, ".identity")); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadIdentity(directory, profile, "values"); err == nil {
		t.Fatal("identity read followed parent symlink")
	}
}

func TestIdentityEnumerationIgnoresUnpublishedTemporaryFile(t *testing.T) {
	directory := privateTempDir(t)
	identityDirectory := filepath.Join(directory, ".identity")
	if err := os.Mkdir(identityDirectory, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(identityDirectory, ".write-interrupted"), []byte("partial"), 0600); err != nil {
		t.Fatal(err)
	}
	items, err := EnumerateIdentities(directory, "tasks")
	if err != nil || len(items) != 0 {
		t.Fatalf("unpublished identity became visible: %#v %v", items, err)
	}
}

func TestProfileKeyRejectsNonCanonicalCase(t *testing.T) {
	key := Key("CaseSensitive")
	if ValidKey("p1-" + strings.ToUpper(key[3:])) {
		t.Fatal("noncanonical uppercase hash accepted")
	}
	if key == Key("casesensitive") {
		t.Fatal("case-sensitive logical identities collided")
	}
}
