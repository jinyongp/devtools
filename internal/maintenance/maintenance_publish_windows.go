// Package maintenance coordinates storage access with recoverable multi-file replacement.
package maintenance

import (
	"encoding/json"
	"errors"

	"os"
)

func recoverLegacy(root string, body []byte) error {
	var entries []before
	if json.Unmarshal(body, &entries) != nil {
		return errors.New("invalid recovery journal")
	}
	for _, entry := range entries {
		if !validPath(entry.Path) {
			return errors.New("invalid recovery path")
		}
	}
	for _, entry := range entries {
		target, err := targetPath(root, entry.Path, true)
		if err != nil {
			return err
		}
		if entry.Exists {
			if err := Write(target, entry.Data); err != nil {
				return err
			}
			continue
		}
		if err := removeTarget(target); err != nil {
			return err
		}
	}
	path := pendingPath(root)
	if err := os.Remove(path); err != nil {
		return err
	}
	return nil
}
