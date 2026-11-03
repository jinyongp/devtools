package cleanup

import "path/filepath"

// Only the configured default cache root is owned here. An absent root never
// means cwd or a parent of a temporary dashboard registry.
func (e Engine) ownedCacheDirectories() []string {
	if !filepath.IsAbs(e.Cache) {
		return nil
	}
	return []string{filepath.Join(e.Cache, "task-queries"), filepath.Join(e.Cache, "collections"), filepath.Join(e.Cache, "dashboard", "queries")}
}
