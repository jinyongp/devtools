package platformfs

import (
	"errors"
	"testing"

	"golang.org/x/sys/windows"
)

func TestWindowsStorageIdentityUsesExactTrustees(t *testing.T) {
	identity, err := currentStorageIdentity()
	if err != nil {
		t.Fatal(err)
	}
	// Model a container independently of this test runner's token. These are
	// distinct package SIDs, not broad capabilities or All Application Packages.
	app, err := windows.StringToSid("S-1-15-2-101-102-103-104-105-106-107")
	if err != nil {
		t.Fatal(err)
	}
	user := identity.owner.String()
	base := "O:" + user + "D:P(A;OICI;FA;;;" + user + ")(A;OICI;FA;;;SY)"
	current := "(A;OICI;FA;;;" + app.String() + ")"
	container := &storageIdentity{owner: identity.owner, trustees: append([]*windows.SID{identity.owner, identity.trustees[1]}, app)}
	ordinary := &storageIdentity{owner: identity.owner, trustees: []*windows.SID{identity.owner, identity.trustees[1]}}
	for _, tc := range []struct {
		name   string
		policy *storageIdentity
		sddl   string
		ok     bool
	}{
		{"ordinary", ordinary, base, true},
		{"specific_container", container, base + current, true},
		{"missing_container", container, base, false},
		{"other_container", container, base + "(A;OICI;FA;;;S-1-15-2-201-202-203-204-205-206-207)", false},
		{"all_packages", container, base + "(A;OICI;FA;;;S-1-15-2-1)", false},
		{"extra_everyone", container, base + current + "(A;OICI;FA;;;WD)", false},
		{"duplicate_user", container, base + "(A;OICI;FA;;;" + user + ")", false},
		{"foreign_container_on_ordinary_storage", ordinary, base + current, false},
		{"read_only_container", container, base + "(A;OICI;FR;;;" + app.String() + ")", false},
		{"inherited_only_container", container, base + "(A;OICIIO;FA;;;" + app.String() + ")", false},
		{"wrong_owner", container, "O:SYD:P(A;OICI;FA;;;" + user + ")(A;OICI;FA;;;SY)" + current, false},
		{"unprotected_directory", container, "O:" + user + "D:(A;OICI;FA;;;" + user + ")(A;OICI;FA;;;SY)" + current, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sd, err := windows.SecurityDescriptorFromString(tc.sddl)
			if err != nil {
				t.Fatal(err)
			}
			err = tc.policy.check(sd, true)
			if tc.ok && err != nil {
				t.Fatal(err)
			}
			if !tc.ok && !errors.Is(err, ErrUnsafe) {
				t.Fatalf("unexpected security verdict: %v", err)
			}
		})
	}
	for _, policy := range []*storageIdentity{ordinary, container} {
		sd, err := policy.descriptor()
		if err != nil {
			t.Fatal(err)
		}
		if err := policy.check(sd, true); err != nil {
			t.Fatal(err)
		}
	}
}
