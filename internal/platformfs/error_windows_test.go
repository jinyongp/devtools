package platformfs

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/jinyongp/devtools/internal/storageerr"
	"golang.org/x/sys/windows"
)

func TestWindowsDirectoryCreationDeniedRetainsOriginalFailure(t *testing.T) {
	parent := filepath.Join(t.TempDir(), "no-create")
	if err := os.Mkdir(parent, 0700); err != nil {
		t.Fatal(err)
	}
	user, full, err := userSecurity()
	if err != nil {
		t.Fatal(err)
	}
	set := func(sd *windows.SECURITY_DESCRIPTOR) {
		t.Helper()
		acl, _, err := sd.DACL()
		if err != nil {
			t.Fatal(err)
		}
		if err := windows.SetNamedSecurityInfo(parent, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION, nil, nil, acl, nil); err != nil {
			t.Fatal(err)
		}
	}
	// Remove FILE_ADD_SUBDIRECTORY from this disposable parent's user ACE.
	limited, err := windows.SecurityDescriptorFromString("D:P(A;OICI;0x1f01fb;;;" + user.String() + ")(A;OICI;FA;;;SY)")
	if err != nil {
		t.Fatal(err)
	}
	set(limited)
	defer set(full)
	root, err := OpenRoot(filepath.Join(parent, "data"), true)
	if root != nil {
		root.Close()
	}
	if !errors.Is(err, windows.ERROR_ACCESS_DENIED) {
		t.Fatalf("expected access denied: %v", err)
	}
	details := storageerr.Details(err, "tasks", "acquire_lock", "task_store")
	if details["operation"] != "create_directory" || details["resource"] != "data_root" || details["reason"] != "access_denied" || details["os_error"] != uint64(5) {
		t.Fatal("original failure lost", details)
	}
}
