package cli

import (
	"github.com/jinyongp/devtools/internal/paths"
	"path/filepath"
)

// Native Known Folder lookup deliberately ignores HOME/XDG. Tests inject their
// owned directories without changing the production resolver or OS trust mode.
func platformTestPaths(app *App, root string) {
	app.userPaths = func() (paths.Directories, error) {
		base := filepath.Dir(root)
		return paths.Directories{Data: root, Config: filepath.Join(base, "config"), Cache: filepath.Join(base, "cache"), Runtime: filepath.Join(base, "runtime")}, nil
	}
}
