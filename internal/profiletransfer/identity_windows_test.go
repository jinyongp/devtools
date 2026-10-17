package profiletransfer

import (
	"context"
	"errors"
	"os"
	"testing"

	"golang.org/x/sys/windows"
)

func TestWindowsRejectsPublicIdentityWithoutRepair(t *testing.T) {
	s := Store{Data: privateRoot(t)}
	if _, err := s.Prepare(context.Background()); err != nil {
		t.Fatal(err)
	}
	sd, err := windows.SecurityDescriptorFromString("D:P(A;;FA;;;WD)")
	if err != nil {
		t.Fatal(err)
	}
	acl, _, err := sd.DACL()
	if err != nil {
		t.Fatal(err)
	}
	if err = windows.SetNamedSecurityInfo(s.identityPath(), windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION, nil, nil, acl, nil); err != nil {
		t.Fatal(err)
	}
	before, err := windows.GetNamedSecurityInfo(s.identityPath(), windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Prepare(context.Background()); !errors.Is(err, ErrInvalidIdentity) {
		t.Fatalf("public identity accepted: %v", err)
	}
	after, err := windows.GetNamedSecurityInfo(s.identityPath(), windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		t.Fatal(err)
	}
	if before.String() != after.String() {
		t.Fatal("public identity was repaired")
	}
}
func TestWindowsRejectsHardlinkedPendingIdentity(t *testing.T) {
	s := Store{Data: privateRoot(t)}
	if _, err := s.Prepare(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(s.identityPath(), s.pendingIdentityPath()); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Prepare(context.Background()); !errors.Is(err, ErrInvalidIdentity) {
		t.Fatalf("hardlinked identity accepted: %v", err)
	}
	if _, err := os.Lstat(s.pendingIdentityPath()); err != nil {
		t.Fatal("ambiguous image removed", err)
	}
}
