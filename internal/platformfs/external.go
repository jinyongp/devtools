package platformfs

import (
	"fmt"
	"io"
	"os"
)

// ReadExternalPrivate checks an artifact without requiring a private parent.
func ReadExternalPrivate(path string, limit int64) ([]byte, error) {
	if limit < 0 || limit == int64(^uint64(0)>>1) {
		return nil, fmt.Errorf("invalid read limit")
	}
	f, err := OpenExternalPrivate(path, os.O_RDONLY, 0)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	if err = CheckFile(f.File); err != nil {
		return nil, err
	}
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if info.Size() > limit {
		return nil, fmt.Errorf("private file exceeds read limit")
	}
	body, err := io.ReadAll(io.LimitReader(f, limit+1))
	if int64(len(body)) > limit {
		return nil, fmt.Errorf("private file exceeds read limit")
	}
	return body, err
}
