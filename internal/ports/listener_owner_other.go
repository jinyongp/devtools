//go:build !linux && !darwin && !windows

package ports

import (
	"context"
	"errors"
)

func listenerProcesses(context.Context, int) ([]ListenerProcess, error) {
	return nil, errors.New("listener observation is unsupported on this platform")
}
