package diagnostics

import (
	"testing"

	"golang.org/x/sys/windows"
)

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
