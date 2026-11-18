// Package privilege invokes constrained devtools helpers. It never executes a
// shell or asks for a password from JSON stdin.
package privilege

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/jinyongp/devtools/internal/protocol"
)

type Runner func(context.Context, string, any, bool) *protocol.Error

const BoundedBodySize = 1 << 20

func encodeRequest(kind string, input any) ([]byte, *protocol.Error) {
	encoded, err := json.Marshal(input)
	if err != nil {
		return nil, protocol.NewError("invalid_argument", "Invalid helper input.", 2, nil)
	}
	return encoded, checkRequestSize(kind, encoded)
}

// CheckRequest checks the same encoding and size limit as the actual helper.
func CheckRequest(kind string, input any) *protocol.Error {
	_, e := encodeRequest(kind, input)
	return e
}

func Run(ctx context.Context, kind string, input any, terminal bool) *protocol.Error {
	if os.Getenv("DEVTOOLS_ISOLATED_ROOT") != "" {
		return protocol.NewError("isolated_operation_not_allowed", "Shared OS changes are unavailable in isolated mode.", 3, nil)
	}
	if kind != "trust" && kind != "hosts" && kind != "netsetup" {
		return protocol.NewError("invalid_argument", "Unknown privileged helper.", 2, nil)
	}
	executable, e := os.Executable()
	if e != nil {
		return protocol.NewError("io_error", "Cannot locate devtools helper.", 1, nil)
	}
	executable, e = filepath.EvalSymlinks(executable)
	if e != nil {
		return protocol.NewError("io_error", "Cannot resolve devtools helper.", 1, nil)
	}
	encoded, problem := encodeRequest(kind, input)
	if problem != nil {
		return problem
	}
	return runPlatform(ctx, kind, encoded, terminal, executable)
}
