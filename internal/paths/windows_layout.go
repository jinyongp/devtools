package paths

import "path/filepath"

func windowsDirectories(localAppData string) Directories {
	base := filepath.Join(localAppData, "devtools")
	return Directories{Config: filepath.Join(base, "config"), Data: filepath.Join(base, "data"), Cache: filepath.Join(base, "cache"), Runtime: filepath.Join(base, "runtime")}
}
