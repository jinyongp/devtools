// Package privilege invokes constrained devtools helpers. It never executes a
// shell or asks for a password from JSON stdin.
package privilege

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/jinyongp/devtools/internal/protocol"
)

type Runner func(context.Context, string, any, bool) *protocol.Error

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
	encoded, e := json.Marshal(input)
	if e != nil {
		return protocol.NewError("invalid_argument", "Invalid helper input.", 2, nil)
	}
	name, args := executable, []string{"__network-helper", kind}
	if os.Geteuid() != 0 {
		name = "sudo"
		args = []string{"--", executable, "__network-helper", kind}
		if !terminal {
			// JSON stdin belongs to the helper, never to sudo authentication.
			// A cached authorization or a configured policy can permit automation.
			args = append([]string{"-n"}, args...)
		}
	}
	command := exec.CommandContext(ctx, name, args...)
	command.Stdin = bytes.NewReader(encoded)
	var output bytes.Buffer
	command.Stdout = &output
	command.Stderr = os.Stderr
	if e := command.Run(); e != nil {
		return protocol.NewError("permission_required", "The limited OS helper did not complete. Noninteractive execution requires preauthorized sudo; inspect diagnostics before retrying.", 3, nil)
	}
	return nil
}
