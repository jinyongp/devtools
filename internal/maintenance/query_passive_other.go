//go:build !linux && !darwin && !windows

package maintenance

import (
	"context"
	"errors"
)

func acquirePassivePair(context.Context, string) (func(), error) {
	return nil, errors.New("passive storage inspection is unsupported on this platform")
}

func acquirePassiveFilePair(context.Context, string) (func(), error) {
	return nil, errors.New("passive storage inspection is unsupported on this platform")
}
