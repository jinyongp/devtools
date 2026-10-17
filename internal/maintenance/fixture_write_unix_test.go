//go:build !windows

package maintenance

import "os"

func writePrivateFixture(path string, body []byte, mode os.FileMode) error {
	return os.WriteFile(path, body, mode)
}
