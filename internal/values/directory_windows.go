package values

import "github.com/jinyongp/devtools/internal/platformfs"

func privateDirectory(path string) error {
	root, err := platformfs.OpenRoot(path, true)
	if err != nil {
		return err
	}
	return root.Close()
}
