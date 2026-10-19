package process

import (
	"strconv"

	"github.com/jinyongp/devtools/internal/protocol"
	"golang.org/x/sys/unix"
)

func nativeExecLimits() (uint64, uint64, uint64, *protocol.Error) {
	limit, err := unix.SysctlUint32("kern.argmax")
	if err != nil || limit == 0 {
		return 0, 0, 0, protocol.NewError("execution_limit_unavailable", "Cannot inspect process input limits.", 3, nil)
	}
	return uint64(limit), 0, uint64(strconv.IntSize / 8), nil
}
