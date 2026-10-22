package projectfile

import (
	"context"
	"golang.org/x/sys/windows"
	"testing"
)

func TestWindowsPublicationPreservesProtectedACL(t *testing.T) {
	path, _ := fileFixture(t)
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		t.Fatal(err)
	}
	sd, err := windows.SecurityDescriptorFromString("D:P(A;;FA;;;" + user.User.Sid.String() + ")(A;;FR;;;SY)")
	if err != nil {
		t.Fatal(err)
	}
	dacl, _, err := sd.DACL()
	if err != nil {
		t.Fatal(err)
	}
	if err = windows.SetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION, nil, nil, dacl, nil); err != nil {
		t.Fatal(err)
	}
	s, e := Read(path)
	if e != nil {
		t.Fatal(e)
	}
	next, e := Publish(context.Background(), s, []byte("profile='next'\n"))
	if e != nil {
		t.Fatal(e, e.Details)
	}
	if s.security != next.security {
		t.Fatal("protected ACL was changed")
	}
}
