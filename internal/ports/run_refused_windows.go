package ports

import (
	"errors"
	"golang.org/x/sys/windows"
)

func refusedConnection(err error) bool {
	return errors.Is(err, windows.WSAECONNREFUSED) || errors.Is(err, windows.WSAEAFNOSUPPORT) || errors.Is(err, windows.WSAEADDRNOTAVAIL)
}
