//go:build !windows

package ports

import (
	"errors"
	"syscall"
)

func refusedConnection(err error) bool {
	return errors.Is(err, syscall.ECONNREFUSED) || errors.Is(err, syscall.EAFNOSUPPORT) || errors.Is(err, syscall.EADDRNOTAVAIL)
}
