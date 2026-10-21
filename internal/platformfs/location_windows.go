package platformfs

import (
	"fmt"
	"path/filepath"
	"strings"

	"golang.org/x/sys/windows"
)

// EntryPathAliased observes the name of an existing entry through this pinned
// root. It opens metadata only, creates nothing and rejects reparse points.
// Its result is diagnostic information, never a file-identity/security decision.
func (r *Root) EntryPathAliased(name string) (bool, error) {
	path, err := r.child(name)
	if err != nil {
		return false, err
	}
	relative, err := filepath.Rel(r.path, path)
	if err != nil || relative == "." {
		return false, fmt.Errorf("%w: relative child entry required", ErrUnsafe)
	}
	parent := r.guard.handles[len(r.guard.handles)-1]
	base, err := normalizedHandlePath(parent, volumeNameNone)
	if err != nil {
		return false, err
	}
	var held []windows.Handle
	defer func() {
		for i := len(held) - 1; i >= 0; i-- {
			_ = windows.CloseHandle(held[i])
		}
	}()
	parts := strings.Split(relative, `\`)
	for i, part := range parts {
		directory := i < len(parts)-1
		h, err := openRelativeMetadataAccess(parent, part, directory, windows.FILE_READ_ATTRIBUTES|windows.READ_CONTROL)
		if err != nil {
			return false, err
		}
		held = append(held, h)
		var info windows.ByHandleFileInformation
		if err = windows.GetFileInformationByHandle(h, &info); err != nil {
			return false, err
		}
		isDirectory := info.FileAttributes&windows.FILE_ATTRIBUTE_DIRECTORY != 0
		if info.FileAttributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 || directory && !isDirectory {
			return false, ErrUnsafe
		}
		if !isDirectory && info.NumberOfLinks != 1 {
			return false, ErrUnsafe
		}
		if err = checkPrivate(h, isDirectory); err != nil {
			return false, err
		}
		if !directory {
			actual, err := normalizedHandlePath(h, volumeNameNone)
			if err != nil {
				return false, err
			}
			return entryPathAliased(base, relative, actual), nil
		}
		parent = h
	}
	return false, ErrUnsafe
}

func entryPathAliased(root, relative, actual string) bool {
	return !strings.EqualFold(filepath.Join(root, relative), actual)
}
