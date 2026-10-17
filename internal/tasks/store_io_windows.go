package tasks

import (
	"context"
	"encoding/json"
	"github.com/jinyongp/devtools/internal/platformfs"
	"path/filepath"
)

func PrivateDir(path string) error {
	root, err := platformfs.OpenRoot(path, true)
	if err != nil {
		return err
	}
	return root.Close()
}
func WritePrivate(path string, v any) error {
	body, err := json.Marshal(v)
	if err != nil {
		return err
	}
	root, err := platformfs.OpenRoot(filepath.Dir(path), false)
	if err != nil {
		return err
	}
	defer root.Close()
	return root.WriteFile(context.Background(), filepath.Base(path), body, true)
}
