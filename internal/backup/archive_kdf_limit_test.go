package backup

import (
	"encoding/base64"
	"fmt"
	"path/filepath"
	"testing"
)

// Mode probing is deliberately authentication-free and never runs a KDF. This
// regression therefore remains cheap even if a future change removes the bound.
func TestArchiveModeRejectsExcessiveOrNoncanonicalKDFBeforePassword(t *testing.T) {
	root := privateTestRoot(t)
	salt := base64.RawStdEncoding.EncodeToString(make([]byte, 16))
	body := base64.RawStdEncoding.EncodeToString(make([]byte, 32))
	for n, factor := range []string{"18", "19", "22", "-1", "18446744073709551616", "+18", "018"} {
		t.Run("factor-"+factor, func(t *testing.T) {
			path := filepath.Join(root, fmt.Sprintf("archive-%d.age", n))
			header := fmt.Sprintf("age-encryption.org/v1\n-> scrypt %s %s\n%s\n--- %s\n", salt, factor, body, body)
			if err := fixtureWriteFile(path, []byte(header), 0600); err != nil {
				t.Fatal(err)
			}
			mode, err := ArchiveEncryptionMode(path)
			if factor == "18" {
				if err != nil || mode != ArchiveEncryptionPassphrase {
					t.Fatal("own export factor not recognized", err)
				}
				return
			}
			if err == nil {
				t.Fatal("unsafe archive reached password selection", factor, mode)
			}
			// Only after cheap probing has actually rejected it, check the direct
			// password decoder too. No large-memory derivation is needed or attempted.
			if _, _, err := decodePassphrase(path, "synthetic-unused-fixture-password"); err == nil {
				t.Fatal("direct decoder accepted unsupported header")
			}
		})
	}
}
