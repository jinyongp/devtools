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

func TestWindowsSecurityComparisonPreservesPermissionIdentity(t *testing.T) {
	compare := func(sddl string) string {
		t.Helper()
		sd, err := windows.SecurityDescriptorFromString(sddl)
		if err != nil {
			t.Fatal(err)
		}
		value, err := comparableSecurity(sd)
		if err != nil {
			t.Fatal(err)
		}
		return value
	}
	base := compare("O:SYG:SYD:AI(A;ID;FR;;;SY)")
	for _, tc := range []struct {
		name, sddl string
		equal      bool
	}{
		{"propagation marker", "O:SYG:SYD:(A;ID;FR;;;SY)", true},
		{"ACE inheritance", "O:SYG:SYD:(A;;FR;;;SY)", false},
		{"protection", "O:SYG:SYD:P(A;ID;FR;;;SY)", false},
		{"permissions", "O:SYG:SYD:(A;ID;FA;;;SY)", false},
		{"owner", "O:BAG:SYD:(A;ID;FR;;;SY)", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if equal := compare(tc.sddl) == base; equal != tc.equal {
				t.Fatalf("permission identity equality=%v, want %v", equal, tc.equal)
			}
		})
	}
}
