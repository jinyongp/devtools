package projectfile

import (
	"bytes"
	"context"
	"encoding/binary"
	"golang.org/x/sys/unix"
	"os"
	"testing"
)

func TestLinuxPublicationPreservesExtendedACL(t *testing.T) {
	path, _ := fileFixture(t)
	var acl bytes.Buffer
	_ = binary.Write(&acl, binary.LittleEndian, uint32(2))
	for _, entry := range []struct {
		Tag, Permission uint16
		ID              uint32
	}{{1, 6, ^uint32(0)}, {2, 4, uint32(os.Getuid())}, {4, 4, ^uint32(0)}, {16, 4, ^uint32(0)}, {32, 0, ^uint32(0)}} {
		if err := binary.Write(&acl, binary.LittleEndian, entry); err != nil {
			t.Fatal(err)
		}
	}
	if err := unix.Setxattr(path, "system.posix_acl_access", acl.Bytes(), 0); err != nil {
		if err == unix.ENOTSUP {
			t.Skip("filesystem has no POSIX ACL support")
		}
		t.Fatalf("ACL bytes %x: %v", acl.Bytes(), err)
	}
	s, e := Read(path)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = Publish(context.Background(), s, []byte("profile='next'\n")); e != nil {
		t.Fatal(e, e.Details)
	}
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	attrs, err := attributes(f)
	if err != nil || !bytes.Equal(attrs["system.posix_acl_access"], acl.Bytes()) {
		t.Fatal("ACL changed", err)
	}
}
