package idref

import (
	"errors"
	"iter"
	"os"
	"strings"
)

// DirectoryNames returns key names without opening individual records.
func DirectoryNames(path string) ([]string, error) {
	directory, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer directory.Close()
	return directory.Readdirnames(-1)
}

func FileKeys(names []string, suffix string) iter.Seq[string] {
	return func(yield func(string) bool) {
		for _, name := range names {
			if id, ok := strings.CutSuffix(name, suffix); ok && !yield(id) {
				return
			}
		}
	}
}
