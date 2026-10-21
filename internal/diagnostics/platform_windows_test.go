package diagnostics

import (
	"errors"
	"os"
	"reflect"
	"testing"

	"golang.org/x/sys/windows"
)

func TestObservedMappingDoesNotDependOnPackageIdentity(t *testing.T) {
	for _, tc := range []struct {
		name    string
		observe func(string) (bool, error)
		want    []string
	}{
		{"same_names", func(string) (bool, error) { return false, nil }, nil},
		{"redirected_lock", func(name string) (bool, error) { return name == `.maintenance\lock.turnstile`, nil }, []string{"windows_storage_path_aliases"}},
		{"missing_entries", func(string) (bool, error) { return false, os.ErrNotExist }, []string{"windows_storage_mapping_unobserved"}},
		{"denied_lock", func(name string) (bool, error) {
			if name == `.maintenance\lock.turnstile` {
				return false, os.ErrPermission
			}
			return false, nil
		}, []string{"windows_storage_mapping_unobserved"}},
		{"mixed", func(name string) (bool, error) {
			if name == "profiles" {
				return false, errors.New("metadata unavailable")
			}
			return true, nil
		}, []string{"windows_storage_path_aliases", "windows_storage_mapping_unobserved"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := observeStorageMapping(tc.observe); !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("got %v want %v", got, tc.want)
			}
		})
	}
}

func TestWindowsPackageIdentityProbe(t *testing.T) {
	got := platformPackageIdentity()
	if got != "packaged" && got != "unpackaged" {
		t.Fatalf("package identity unavailable on a supported Windows host: %s", got)
	}
	t.Log("package_identity=" + got)
}

func TestWindowsPackageIdentityStatus(t *testing.T) {
	for _, tc := range []struct {
		status uintptr
		size   uint32
		want   string
	}{
		{uintptr(windows.ERROR_INSUFFICIENT_BUFFER), 64, "packaged"},
		{uintptr(windows.APPMODEL_ERROR_NO_PACKAGE), 0, "unpackaged"},
		{uintptr(windows.ERROR_ACCESS_DENIED), 0, "unknown"},
		{uintptr(windows.ERROR_INSUFFICIENT_BUFFER), 0, "unknown"},
		{0, 0, "unknown"},
	} {
		if got := packageIdentityFromStatus(tc.status, tc.size); got != tc.want {
			t.Fatalf("status=%d size=%d: got %s, want %s", tc.status, tc.size, got, tc.want)
		}
	}
}
