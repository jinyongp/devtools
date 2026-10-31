//go:build !linux && !darwin && !windows

package ports

import "fmt"

func runProcessIdentity(int) (string, error) {
	return "", fmt.Errorf("process identity is unavailable")
}
