package maintenance

import (
	"github.com/jinyongp/devtools/internal/platformfs"
	"os"
	"path/filepath"
)

func writePrivateFixture(path string, body []byte, _ os.FileMode) error {
	root, err := platformfs.OpenRoot(filepath.Dir(path), false)
	if err != nil {
		return err
	}
	defer root.Close()
	f, err := root.OpenFile(filepath.Base(path), os.O_CREATE|os.O_WRONLY|os.O_TRUNC)
	if err != nil {
		return err
	}
	_, err = f.Write(body)
	closeErr := f.Close()
	if err != nil {
		return err
	}
	return closeErr
}
