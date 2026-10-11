// Package paths computes user directories without creating them.
package paths

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

type Directories struct {
	Config       string `json:"config"`
	Data         string `json:"data"`
	Cache        string `json:"cache"`
	Runtime      string `json:"runtime,omitempty"`
	IsolatedRoot string `json:"-"`
}

func Current() (Directories, error) {
	if root := os.Getenv("DEVTOOLS_ISOLATED_ROOT"); root != "" {
		return Isolated(root)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return Directories{}, err
	}
	return Resolve(runtime.GOOS, home, os.Getenv)
}

// Isolated resolves a root without creating it or changing process environment.
func Isolated(root string) (Directories, error) {
	if root == "" {
		return Directories{}, fmt.Errorf("isolation root is empty")
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return Directories{}, err
	}
	// Resolve existing ancestors so aliases cannot accidentally select two daemons.
	ancestor, suffix := abs, []string{}
	for {
		if resolved, e := filepath.EvalSymlinks(ancestor); e == nil {
			abs = resolved
			for i := len(suffix) - 1; i >= 0; i-- {
				abs = filepath.Join(abs, suffix[i])
			}
			break
		} else if !os.IsNotExist(e) {
			return Directories{}, e
		}
		parent := filepath.Dir(ancestor)
		if parent == ancestor {
			return Directories{}, fmt.Errorf("cannot resolve isolation root")
		}
		suffix = append(suffix, filepath.Base(ancestor))
		ancestor = parent
	}
	return Directories{Config: filepath.Join(abs, "config"), Data: filepath.Join(abs, "data"), Cache: filepath.Join(abs, "cache"), Runtime: filepath.Join(abs, "runtime"), IsolatedRoot: abs}, nil
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
