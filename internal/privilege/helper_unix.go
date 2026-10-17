//go:build !windows

package privilege

import (
	"bytes"
	"context"
	"github.com/jinyongp/devtools/internal/protocol"
	"os"
	"os/exec"
)

func runPlatform(ctx context.Context, kind string, encoded []byte, terminal bool, executable string) *protocol.Error {
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
