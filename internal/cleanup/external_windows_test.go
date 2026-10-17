package cleanup

import (
	"context"
	"github.com/jinyongp/devtools/internal/tasks"
	"golang.org/x/sys/windows"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestWindowsOldBackupInPublicDirectoryRoundTrip(t *testing.T) {
	root := privateTestRoot(t)
	e := Engine{Data: filepath.Join(root, "data"), Cache: filepath.Join(root, "cache"), Config: filepath.Join(root, "config")}
	parent := filepath.Join(t.TempDir(), "public")
	if err := os.Mkdir(parent, 0755); err != nil {
		t.Fatal(err)
	}
	before, err := windows.GetNamedSecurityInfo(parent, windows.SE_FILE_OBJECT, windows.OWNER_SECURITY_INFORMATION|windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		t.Fatal(err)
	}
	if err := write(filepath.Join(e.Config, "backup.json"), map[string]any{"directory": parent}); err != nil {
		t.Fatal(err)
	}
	var oldest string
	for i := 0; i < 4; i++ {
		path := filepath.Join(parent, "devtools-"+tasks.ID()+".age")
		if err := fixtureWriteFile(path, []byte("encrypted-backup"), 0600); err != nil {
			t.Fatal(err)
		}
		age := time.Now().Add(-time.Duration(40-i) * 24 * time.Hour)
		if err := os.Chtimes(path, age, age); err != nil {
			t.Fatal(err)
		}
		if i == 0 {
			oldest = path
		}
	}
	plan, failure := e.Preview(context.Background(), "")
	if failure != nil || len(plan.Items) != 1 || plan.Items[0].Source != oldest {
		t.Fatalf("preview: %#v %v", plan, failure)
	}
	id := plan.Items[0].ID
	if _, failure = e.Apply(context.Background(), plan.ID, []string{id}, tasks.ID()); failure != nil {
		t.Fatal(failure)
	}
	if _, err = os.Stat(oldest); !os.IsNotExist(err) {
		t.Fatalf("archive source remains: %v", err)
	}
	if _, changed, failure := e.Restore(context.Background(), id); failure != nil || !changed {
		t.Fatalf("restore: %v %v", changed, failure)
	}
	body, err := readCleanupSource("old_backup", oldest, 128)
	if err != nil || string(body) != "encrypted-backup" {
		t.Fatalf("round trip: %q %v", body, err)
	}
	after, err := windows.GetNamedSecurityInfo(parent, windows.SE_FILE_OBJECT, windows.OWNER_SECURITY_INFORMATION|windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		t.Fatal(err)
	}
	if before.String() != after.String() {
		t.Fatal("cleanup repaired public artifact parent")
	}
}
