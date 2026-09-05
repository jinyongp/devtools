// Package paths computes user directories without creating them.
package paths

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

type Directories struct {
	Config string `json:"config"`
	Data   string `json:"data"`
	Cache  string `json:"cache"`
}

func Current() (Directories, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return Directories{}, err
	}
	return Resolve(runtime.GOOS, home, os.Getenv)
}

// Resolve treats WSL as Linux. Relative XDG values are ignored.
func Resolve(goos, home string, getenv func(string) string) (Directories, error) {
	if !filepath.IsAbs(home) {
		return Directories{}, fmt.Errorf("home directory must be absolute")
	}
	switch goos {
	case "darwin":
		base := filepath.Join(home, "Library", "Application Support", "devtools")
		return Directories{Config: base, Data: filepath.Join(base, "data"), Cache: filepath.Join(home, "Library", "Caches", "devtools")}, nil
	case "linux":
		base := func(key, fallback string) string {
			value := getenv(key)
			if !filepath.IsAbs(value) {
				value = filepath.Join(home, fallback)
			}
			return filepath.Join(value, "devtools")
		}
		return Directories{Config: base("XDG_CONFIG_HOME", ".config"), Data: base("XDG_DATA_HOME", ".local/share"), Cache: base("XDG_CACHE_HOME", ".cache")}, nil
	default:
		return Directories{}, fmt.Errorf("unsupported operating system: %s", goos)
	}
}
