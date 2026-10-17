package tasks

import (
	"github.com/jinyongp/devtools/internal/platformfs"
	"os"
)

// Every metadata image is flushed and published WRITE_THROUGH before this
// boundary. Flush the WAL itself before the head/marker transaction; Windows
// does not claim a directory fsync or survival of power failure.
func generationDurable(resolution v3Resolution) error {
	file, err := platformfs.OpenPrivate(resolution.WAL, os.O_RDWR, 0)
	if err != nil {
		return err
	}
	defer file.Close()
	if err = platformfs.CheckFile(file.File); err != nil {
		return err
	}
	return file.Sync()
}
