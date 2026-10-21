package diagnostics

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/jinyongp/devtools/internal/paths"
)

type Platform struct {
	Isolation         string   `json:"isolation"`
	OS                string   `json:"os"`
	Arch              string   `json:"arch"`
	Runtime           string   `json:"runtime"`
	StorageFilesystem string   `json:"storage_filesystem"`
	StorageSupported  bool     `json:"storage_supported"`
	Interop           string   `json:"interop"`
	ClientTrust       string   `json:"client_trust"`
	Limitations       []string `json:"limitations"`
}

// Only bounded, fixed metadata sources are inspected. No child process, shell,
// network probe, raw environment dump or WSL configuration read is performed.
func inspectPlatform(input Input) (Platform, bool) {
	platform, unsupported := platformSnapshot(input, runtime.GOOS, runtime.GOARCH, os.Getenv, readPlatformMetadata, storageFilesystem)
	platform.Isolation = platformIsolation()
	platform = describeStorageScope(platform, storagePathSource(input))
	return platform, unsupported
}

func describeStorageScope(platform Platform, source string) Platform {
	// Preserve the public shape: this is a scope limitation, not a health
	// failure. An empty container store says nothing about the host store.
	if platform.OS == "windows" && platform.Isolation == "appcontainer" && source == "windows_known_folder" {
		platform.Limitations = append(platform.Limitations, "appcontainer_known_folder_store")
	}
	return platform
}
func platformSnapshot(input Input, goos, arch string, getenv func(string) string, read func(string) (string, error), filesystem func(string) (string, bool, bool)) (Platform, bool) {
	p := Platform{OS: goos, Arch: arch, Runtime: "native", Isolation: "unknown", Interop: "not_applicable", ClientTrust: "local_only", Limitations: []string{}}
	if isWSL(goos, getenv, read) {
		p.Runtime = "wsl"
		p.Interop = wslInterop(read)
		p.ClientTrust = "separate_windows_store"
		p.Limitations = []string{"wsl_windows_browser_not_verified", "wsl_network_mode_unknown", "wsl_ipv6_reverse_lan_not_verified", "wsl_arm64_not_verified", "wsl_managed_win32_unsupported"}
	}
	unsupported := false
	for i, path := range []string{input.Data, input.Config, input.Cache} {
		fs, supported, known := filesystem(path)
		if i == 0 {
			p.StorageFilesystem = fs
			p.StorageSupported = supported
		}
		if known && !supported {
			unsupported = true
		}
		if !known && !containsPlatformLimit(p.Limitations, "storage_filesystem_unknown") {
			p.Limitations = append(p.Limitations, "storage_filesystem_unknown")
		}
	}
	return p, unsupported
}

func storagePathSource(input Input) string {
	if input.IsolatedRoot != "" {
		return "isolated_root"
	}
	current, err := paths.Current()
	if err != nil || current.Data != input.Data || current.Config != input.Config || current.Cache != input.Cache {
		return "provided"
	}
	if runtime.GOOS == "windows" {
		return "windows_known_folder"
	}
	return "user_directories"
}
func containsPlatformLimit(items []string, value string) bool {
	for _, item := range items {
		if item == value {
			return true
		}
	}
	return false
}
func readPlatformMetadata(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	body, err := io.ReadAll(io.LimitReader(file, 4097))
	if err != nil {
		return "", err
	}
	if len(body) > 4096 {
		return "", errors.New("metadata too large")
	}
	return string(body), nil
}
func isWSL(goos string, getenv func(string) string, read func(string) (string, error)) bool {
	if goos != "linux" {
		return false
	}
	if getenv("WSL_DISTRO_NAME") != "" || getenv("WSL_INTEROP") != "" {
		return true
	}
	release, err := read("/proc/sys/kernel/osrelease")
	return err == nil && strings.Contains(strings.ToLower(release), "microsoft")
}
func wslInterop(read func(string) (string, error)) string {
	for _, path := range []string{"/proc/sys/fs/binfmt_misc/status", "/proc/sys/fs/binfmt_misc/WSLInterop"} {
		body, err := read(path)
		if err == nil && strings.TrimSpace(strings.SplitN(body, "\n", 2)[0]) == "disabled" {
			return "unavailable"
		}
	}
	// An enabled registration or an existing .exe cannot establish that an
	// execution would succeed. Diagnostics deliberately does not try one.
	return "unknown"
}
func existingPlatformAncestor(path string) (string, error) {
	for {
		_, err := os.Stat(path)
		if err == nil {
			return path, nil
		}
		if !errors.Is(err, os.ErrNotExist) {
			return "", err
		}
		parent := filepath.Dir(path)
		if parent == path {
			return "", err
		}
		path = parent
	}
}
