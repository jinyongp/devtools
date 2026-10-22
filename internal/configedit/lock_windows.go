package configedit

import (
	"github.com/jinyongp/devtools/internal/location"
	"strings"
)

func lockKey(path string) string {
	canonical, err := location.Canonical(path)
	if err != nil {
		canonical = path
	}
	return strings.ToLower(canonical)
}
