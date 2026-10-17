package services

import (
	"context"
	"errors"
)

func acquireLogLock(ctx context.Context, logPath string, exclusive bool) (func(), error) {
	return nil, errors.ErrUnsupported
}
