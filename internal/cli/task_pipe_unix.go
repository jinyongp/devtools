//go:build !windows

package cli

import (
	"context"
	"os"
)

func readTaskPipe(context.Context, *os.File, int) ([]byte, error, bool) { return nil, nil, false }
