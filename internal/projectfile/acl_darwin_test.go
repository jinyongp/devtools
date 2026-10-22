package projectfile

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"syscall"
	"testing"
)

func TestDarwinPublicationPreservesExtendedACL(t *testing.T) {
	path, _ := fileFixture(t)
	if out, err := exec.Command("/bin/chmod", "+a", "everyone allow read", path).CombinedOutput(); err != nil {
		t.Fatalf("set native ACL: %v %s", err, out)
	}
	s, e := Read(path)
	if e != nil {
		t.Fatalf("read ACL fixture: code=%s details=%v", e.Code, e.Details)
	}
	next, e := Publish(context.Background(), s, []byte("profile='next'\n"))
	if e != nil {
		t.Fatal(e, e.Details)
	}
	if s.security != next.security {
		t.Fatal("extended ACL was changed")
	}
}

func TestDarwinACLReadRejectsClosedDescriptor(t *testing.T) {
	path, _ := fileFixture(t)
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err = f.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err = getACL(f); !errors.Is(err, syscall.EBADF) {
		t.Fatalf("invalid descriptor was treated as an absent ACL: %v", err)
	}
	failure := ioFailure("read.metadata", err)
	facts := failure.Details["facts"].(map[string]any)
	if facts["stage"] != "read.metadata.acl_get_fd_np" || facts["os_code"] != uint64(syscall.EBADF) {
		t.Fatal("ACL failure lost its OS diagnosis", failure.Details)
	}
}
