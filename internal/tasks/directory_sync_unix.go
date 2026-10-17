//go:build !windows

package tasks

import (
	"os"
)

func syncPrivateDirectory(path string) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	return file.Sync()
}
