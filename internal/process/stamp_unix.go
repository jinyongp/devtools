//go:build !windows

package process

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"syscall"
)

// ExecutableStamp detects observed replacement/metadata changes. It is a
// preparation guard, not a content signature or security attestation.
func ExecutableStamp(path string) (string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	st := info.Sys().(*syscall.Stat_t)
	sum := sha256.Sum256([]byte(fmt.Sprintf("%d:%d:%d:%d:%d:%d:%d", st.Dev, st.Ino, info.Size(), info.ModTime().UnixNano(), info.Mode(), st.Uid, st.Gid)))
	return hex.EncodeToString(sum[:]), nil
}
