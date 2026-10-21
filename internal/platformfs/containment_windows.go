package platformfs

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Verify the requested entry through the pinned root, not by comparing names
// returned for independently opened handles. Normalized names are not file IDs.
// Walk one component at a time so no intermediate reparse point is followed.
func containedFile(r *Root, f *os.File, path string) (err error) {
	defer func() { err = fsError("verify_file_identity", path, "", err) }()
	relative, err := filepath.Rel(r.path, path)
	if err != nil || relative == "." || !filepath.IsLocal(relative) {
		return fsError("verify_file_identity", path, "path_mismatch", ErrUnsafe)
	}
	parent := r.guard.handles[len(r.guard.handles)-1]
	var held []windows.Handle
	defer func() {
		for i := len(held) - 1; i >= 0; i-- {
			_ = windows.CloseHandle(held[i])
		}
	}()
	parts := strings.Split(relative, `\`)
	for i, part := range parts {
		directory := i < len(parts)-1
		h, e := openRelativeMetadata(parent, part, directory)
		if e != nil {
			return e
		}
		held = append(held, h)
		var expected windows.ByHandleFileInformation
		if e = windows.GetFileInformationByHandle(h, &expected); e != nil {
			return e
		}
		if expected.FileAttributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 {
			if directory {
				return fsError("verify_directory_identity", path, "reparse_or_non_directory", ErrUnsafe)
			}
			return fsError("verify_file_identity", path, "reparse_or_non_regular_file", ErrUnsafe)
		}
		if directory {
			if expected.FileAttributes&windows.FILE_ATTRIBUTE_DIRECTORY == 0 {
				return fsError("verify_directory_identity", path, "reparse_or_non_directory", ErrUnsafe)
			}
			parent = h
			continue
		}
		var actual windows.ByHandleFileInformation
		if e = windows.GetFileInformationByHandle(windows.Handle(f.Fd()), &actual); e != nil {
			return e
		}
		if expected.FileAttributes&windows.FILE_ATTRIBUTE_DIRECTORY != 0 || actual.FileAttributes&(windows.FILE_ATTRIBUTE_DIRECTORY|windows.FILE_ATTRIBUTE_REPARSE_POINT) != 0 {
			return fsError("verify_file_identity", path, "reparse_or_non_regular_file", ErrUnsafe)
		}
		if expected.NumberOfLinks != 1 || actual.NumberOfLinks != 1 {
			return fsError("verify_file_identity", path, "hard_link", ErrUnsafe)
		}
		if expected.VolumeSerialNumber != actual.VolumeSerialNumber || expected.FileIndexHigh != actual.FileIndexHigh || expected.FileIndexLow != actual.FileIndexLow {
			return fsError("verify_file_identity", path, "file_identity_mismatch", ErrUnsafe)
		}
	}
	return nil
}

func openRelativeMetadata(parent windows.Handle, name string, directory bool) (windows.Handle, error) {
	return openRelativeMetadataAccess(parent, name, directory, windows.FILE_READ_ATTRIBUTES)
}

func openRelativeMetadataAccess(parent windows.Handle, name string, directory bool, access uint32) (windows.Handle, error) {
	objectName, err := windows.NewNTUnicodeString(name)
	if err != nil {
		return 0, err
	}
	attributes := windows.OBJECT_ATTRIBUTES{
		RootDirectory: parent,
		ObjectName:    objectName,
		// Use the filesystem's lookup rules, including per-directory NTFS case
		// sensitivity, just as CreateFile does. Never fold the returned names.
		Attributes: windows.OBJ_CASE_INSENSITIVE,
	}
	attributes.Length = uint32(unsafe.Sizeof(attributes))
	share := uint32(windows.FILE_SHARE_READ | windows.FILE_SHARE_WRITE | windows.FILE_SHARE_DELETE)
	if directory {
		share &^= windows.FILE_SHARE_DELETE
	}
	var handle windows.Handle
	var status windows.IO_STATUS_BLOCK
	err = windows.NtCreateFile(&handle, access, &attributes, &status,
		nil, 0, share, windows.FILE_OPEN, windows.FILE_OPEN_REPARSE_POINT, 0, 0)
	if err != nil {
		var nt windows.NTStatus
		if errors.As(err, &nt) {
			return 0, nt.Errno()
		}
		return 0, err
	}
	return handle, nil
}
