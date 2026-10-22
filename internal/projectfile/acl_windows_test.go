package projectfile

import (
	"context"
	"golang.org/x/sys/windows"
	"os"
	"path/filepath"
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

func TestWindowsReplacementPreservesSnapshotMetadata(t *testing.T) {
	path, source := fileFixture(t)
	f, err := os.CreateTemp(filepath.Dir(path), ".replacement-*")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	defer os.Remove(f.Name())
	if err = copyMetadata(source, f); err != nil {
		t.Fatal(err)
	}
	if _, err = f.Write([]byte("profile='next'\n")); err != nil {
		t.Fatal(err)
	}
	if err = f.Close(); err != nil {
		t.Fatal(err)
	}
	candidate, failure := Read(f.Name())
	if failure != nil {
		t.Fatal(failure, failure.Details)
	}
	committed, uncertain, backup, err := replace(context.Background(), source, candidate)
	defer os.Remove(backup)
	if err != nil || !committed || uncertain {
		t.Fatalf("replacement: committed=%v uncertain=%v error=%v", committed, uncertain, err)
	}
	observed, failure := Read(path)
	if failure != nil {
		t.Fatal(failure, failure.Details)
	}
	if observed.security != source.security {
		t.Fatalf("permission identity changed: source=%q candidate=%q observed=%q", source.security, candidate.security, observed.security)
	}
	if observed.identity != candidate.identity || !os.SameFile(observed.Info, candidate.Info) {
		t.Fatalf("file identity changed: candidate=%q observed=%q same_file=%v", candidate.identity, observed.identity, os.SameFile(observed.Info, candidate.Info))
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
