//go:build !windows

package configedit

import "github.com/jinyongp/devtools/internal/location"

func lockKey(path string) string {
	canonical, err := location.Canonical(path)
	if err != nil {
		return path
	}
	return canonical
}
