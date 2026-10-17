//go:build !windows

package profilekey

import "os"

func writePrivateFixture(path string, body []byte, mode os.FileMode) error {
	return os.WriteFile(path, body, mode)
}
