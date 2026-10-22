package project

import (
	"errors"
	"os"
	"path/filepath"

	"github.com/jinyongp/devtools/internal/location"
	"github.com/jinyongp/devtools/internal/protocol"
)

// Find locates even an invalid configuration so check/fmt can report its source.
func Find(start string) (string, *protocol.Error) {
	dir, err := location.ExistingDirectory(start)
	if err != nil {
		return "", ioError(start, err)
	}
	for {
		path := filepath.Join(dir, Filename)
		if _, err := os.Lstat(path); err == nil {
			return path, nil
		} else if !errors.Is(err, os.ErrNotExist) {
			return "", ioError(path, err)
		}
		if _, err := os.Lstat(filepath.Join(dir, ".git")); err == nil {
			break
		} else if !errors.Is(err, os.ErrNotExist) {
			return "", ioError(dir, err)
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return "", protocol.NewError("project_not_found", "No devtools.toml was found within the project boundary. Use --dir or devtools init --profile NAME.", 3, map[string]any{"reason": "not_found"})
}
