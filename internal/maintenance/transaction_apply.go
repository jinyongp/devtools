package maintenance

import (
	"sort"
)

func applyReplacement(root string, files map[string][]byte) error {
	paths := make([]string, 0, len(files))
	for path := range files {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	for _, relative := range paths {
		target, err := targetPath(root, relative, true)
		if err != nil {
			return err
		}
		data := files[relative]
		if data == nil {
			if err := removeTarget(target); err != nil {
				return err
			}
			continue
		}
		if err := Write(target, data); err != nil {
			return err
		}
	}
	return nil
}
