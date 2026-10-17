package platformfs

import (
	"errors"
	"os"
)

func OpenNoFollow(path string, flags int, perm os.FileMode) (*os.File, error) {
	return nil, &os.PathError{Op: "open", Path: path, Err: errors.ErrUnsupported}
}
