package process

import (
	"os"
	"strconv"

	"github.com/jinyongp/devtools/internal/protocol"
	"golang.org/x/sys/unix"
)

func nativeExecLimits() (uint64, uint64, uint64, *protocol.Error) {
	var stack unix.Rlimit
	if unix.Getrlimit(unix.RLIMIT_STACK, &stack) != nil {
		return 0, 0, 0, protocol.NewError("execution_limit_unavailable", "Cannot inspect process input limits.", 3, nil)
	}
	limit := min(uint64(6<<20), stack.Cur/4)
	limit = max(limit, uint64(32*os.Getpagesize()))
	return limit, uint64(32 * os.Getpagesize()), uint64(strconv.IntSize / 8), nil
}
