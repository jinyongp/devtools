package process

import (
	"context"
	"errors"
	"github.com/jinyongp/devtools/internal/protocol"
	"io"
	"os/exec"
)

// W3/W4 replace these explicit interim failures with the native launcher.
func Execute(ctx context.Context, args []string, dir string, env []string, in io.Reader, out, diagnostic io.Writer) (int, *protocol.Error) {
	return 0, protocol.NewError("unsupported_platform", "Windows process execution is not implemented in this intermediate build.", 3, nil)
}
func StartDetached(command *exec.Cmd) error { return errors.ErrUnsupported }
