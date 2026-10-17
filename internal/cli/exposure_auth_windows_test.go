package cli

import (
	"context"
	"github.com/jinyongp/devtools/internal/platformfs"
	"os"
	"path/filepath"
	"testing"
)

func TestWindowsExposureAuthFileRequiresPrivateACL(t *testing.T) {
	public := t.TempDir()
	path := filepath.Join(public, "authentication.txt")
	if err := platformfs.WriteExternalPrivate(context.Background(), path, []byte("alice:fixture-password\r\n"), false); err != nil {
		t.Fatal(err)
	}
	auth, failure := readExposureAuth(IO{}, Request{Options: map[string]string{"auth-file": path}})
	if failure != nil || auth != "alice:fixture-password" {
		t.Fatal("private external authentication unavailable", failure)
	}
	insecure := filepath.Join(public, "public.txt")
	if err := os.WriteFile(insecure, []byte("must-not-use"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, failure := readExposureAuth(IO{}, Request{Options: map[string]string{"auth-file": insecure}}); failure == nil || failure.Code != "invalid_authentication_file" {
		t.Fatal("public ACL accepted", failure)
	}
}
