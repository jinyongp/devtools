package testfs

import (
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/windows"
)

func canonicalDir(path string) (string, error) {
	// Unlike a product private-storage open, this follows aliases of an owned
	// fixture on purpose. Go's EvalSymlinks can leave directory junctions intact.
	directory, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer directory.Close()
	buffer := make([]uint16, 512)
	for {
		n, err := windows.GetFinalPathNameByHandle(windows.Handle(directory.Fd()), &buffer[0], uint32(len(buffer)), 0)
		if err != nil {
			return "", err
		}
		if n >= uint32(len(buffer)) {
			buffer = make([]uint16, n+1)
			continue
		}
		physical := windows.UTF16ToString(buffer[:n])
		if strings.HasPrefix(physical, `\\?\UNC\`) {
			physical = `\\` + strings.TrimPrefix(physical, `\\?\UNC\`)
		} else {
			physical = strings.TrimPrefix(physical, `\\?\`)
		}
		return filepath.Clean(physical), nil
	}
}
