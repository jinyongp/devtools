package diagnostics

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestStorageScopeDoesNotConfuseExplicitAndDefaultStores(t *testing.T) {
	for _, tc := range []struct {
		os, isolation, source string
		containerDefault      bool
	}{
		{"windows", "appcontainer", "windows_known_folder", true},
		{"windows", "appcontainer", "isolated_root", false},
		{"windows", "appcontainer", "provided", false},
		{"windows", "unrestricted", "windows_known_folder", false},
		{"windows", "restricted", "windows_known_folder", false},
		{"linux", "unknown", "user_directories", false},
	} {
		p := describeStorageScope(Platform{OS: tc.os, Isolation: tc.isolation, Limitations: []string{"existing_limit"}}, tc.source, "unpackaged")
		if containsPlatformLimit(p.Limitations, "appcontainer_known_folder_store") != tc.containerDefault || p.Limitations[0] != "existing_limit" {
			t.Fatal(tc, p)
		}
	}
}

func TestPackageStorageScopeIsIndependentOfTokenIsolation(t *testing.T) {
	for _, tc := range []struct {
		os, isolation, source, identity, want string
	}{
		{"windows", "unrestricted", "windows_known_folder", "packaged", "packaged_windows_default_store"},
		{"windows", "appcontainer", "windows_known_folder", "packaged", "packaged_windows_default_store"},
		{"windows", "unrestricted", "windows_known_folder", "unknown", "windows_package_identity_unknown"},
		{"windows", "unrestricted", "windows_known_folder", "unpackaged", ""},
		{"windows", "unrestricted", "isolated_root", "packaged", ""},
		{"windows", "unrestricted", "provided", "packaged", ""},
		{"linux", "unknown", "user_directories", "packaged", ""},
	} {
		p := describeStorageScope(Platform{OS: tc.os, Isolation: tc.isolation, Limitations: []string{"existing_limit"}}, tc.source, tc.identity)
		for _, code := range []string{"packaged_windows_default_store", "windows_package_identity_unknown"} {
			if containsPlatformLimit(p.Limitations, code) != (tc.want == code) {
				t.Fatal(tc, p)
			}
		}
		if p.Isolation != tc.isolation || p.Limitations[0] != "existing_limit" {
			t.Fatal("storage scope changed token isolation or existing limitations", tc, p)
		}
	}
}

func TestPlatformWSLReadOnlyUnknownAndSeparateTrust(t *testing.T) {
	reads := []string{}
	read := func(path string) (string, error) {
		reads = append(reads, path)
		if strings.HasSuffix(path, "osrelease") {
			return "6.6.1-microsoft-standard-WSL2", nil
		}
		return "enabled\ninterpreter /init", nil
	}
	fs := func(path string) (string, bool, bool) {
		if strings.HasPrefix(path, "/mnt/") {
			t.Error("project path used as private storage")
		}
		return "ext", true, true
	}
	getenv := func(key string) string {
		if key == "WSLENV" {
			return "SECRET_CANARY"
		}
		return ""
	}
	p, unsupported := platformSnapshot(Input{Data: "/home/u/data", Config: "/home/u/config", Cache: "/home/u/cache"}, "linux", "amd64", getenv, read, fs)
	if unsupported || p.Runtime != "wsl" || p.Interop != "unknown" || p.ClientTrust != "separate_windows_store" || !p.StorageSupported {
		t.Fatal(p, unsupported)
	}
	want := []string{"/proc/sys/kernel/osrelease", "/proc/sys/fs/binfmt_misc/status", "/proc/sys/fs/binfmt_misc/WSLInterop"}
	if !reflect.DeepEqual(reads, want) {
		t.Fatal("unexpected metadata sources", reads)
	}
	body, _ := json.Marshal(p)
	if strings.Contains(string(body), "SECRET_CANARY") || strings.Contains(string(body), "interpreter") {
		t.Fatal("raw metadata leaked")
	}
	for _, code := range []string{"wsl_windows_browser_not_verified", "wsl_network_mode_unknown", "wsl_ipv6_reverse_lan_not_verified", "wsl_arm64_not_verified"} {
		if !containsPlatformLimit(p.Limitations, code) {
			t.Fatal("omitted limit", code)
		}
	}
}
func TestPlatformExplicitDisabledAndMissingInterop(t *testing.T) {
	for _, tc := range []struct{ body, want string }{{"disabled\n", "unavailable"}, {"enabled\n", "unknown"}, {"", "unknown"}} {
		if got := wslInterop(func(string) (string, error) {
			if tc.body == "" {
				return "", os.ErrNotExist
			}
			return tc.body, nil
		}); got != tc.want {
			t.Fatal(got, tc.want)
		}
	}
	if isWSL("windows", func(string) string { return "spoof" }, func(string) (string, error) { t.Fatal("Windows read procfs"); return "", nil }) {
		t.Fatal("Windows marked WSL")
	}
}
func TestPlatformUnsupportedCacheAndUnknownFilesystem(t *testing.T) {
	input := Input{Data: "data", Config: "config", Cache: "cache"}
	fs := func(path string) (string, bool, bool) {
		if path == "cache" {
			return "9p", false, true
		}
		return "ext", true, true
	}
	p, unsupported := platformSnapshot(input, "linux", "arm64", func(string) string { return "" }, func(string) (string, error) { return "", os.ErrNotExist }, fs)
	if !unsupported || !p.StorageSupported || p.Interop != "not_applicable" {
		t.Fatal(p, unsupported)
	}
	p, unsupported = platformSnapshot(input, "linux", "amd64", func(string) string { return "" }, func(string) (string, error) { return "", os.ErrNotExist }, func(string) (string, bool, bool) { return "unknown", false, false })
	if unsupported || !containsPlatformLimit(p.Limitations, "storage_filesystem_unknown") {
		t.Fatal(p, unsupported)
	}
}
func TestPlatformAncestorInspectionCreatesNothing(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "missing", "data")
	if path, err := existingPlatformAncestor(target); err != nil || path != root {
		t.Fatal(path, err)
	}
	if _, err := os.Stat(target); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("inspection created path", err)
	}
	if name, _, known := storageFilesystem(target); name == "" || !known {
		t.Fatal("fixture filesystem unavailable", name)
	}
}
