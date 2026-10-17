package process

import (
	"context"
	"errors"
	"github.com/jinyongp/devtools/internal/location"
	"github.com/jinyongp/devtools/internal/platformfs"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/jinyongp/devtools/internal/protocol"
	"golang.org/x/sys/windows"
)

// prepareInvocation is shared by argv tests and the native launcher. The Job
// owner and console cancellation implementation are added by W3C/W4P.
func prepareInvocation(ctx context.Context, args []string, dir string, env []string) (*exec.Cmd, *protocol.Error) {
	if len(args) == 0 {
		return nil, protocol.NewError("invalid_argument", "Specify a command to execute.", 2, nil)
	}
	for _, arg := range args {
		if strings.ContainsRune(arg, '\x00') {
			return nil, protocol.NewError("invalid_argument", "Command arguments cannot contain NUL.", 2, nil)
		}
	}
	canonical, err := location.ExistingDirectory(dir)
	if err != nil {
		if errors.Is(err, platformfs.ErrUnsafe) {
			return nil, protocol.NewError("invalid_argument", "Windows execution requires a local NTFS directory.", 2, nil)
		}
		return nil, protocol.NewError("command_not_found", "Execution directory was not found.", 127, nil)
	}
	dir = canonical
	path, e := LookPath(args[0], dir, env)
	if e != nil {
		return nil, e
	}
	if strings.EqualFold(filepath.Ext(path), ".ps1") {
		return nil, protocol.NewError("invalid_argument", "Run PowerShell scripts with powershell.exe/pwsh -File explicitly.", 2, nil)
	}
	cmd := exec.CommandContext(ctx, path, args[1:]...)
	cmd.Args[0] = args[0]
	if ext := filepath.Ext(path); strings.EqualFold(ext, ".cmd") || strings.EqualFold(ext, ".bat") {
		literal := func(arg string) (string, bool) {
			if strings.ContainsAny(arg, "\x00\r\n\"%") {
				return "", false
			}
			trailing := len(arg) - len(strings.TrimRight(arg, `\`))
			return `"` + arg + strings.Repeat(`\`, trailing) + `"`, true
		}
		script, ok := literal(path)
		if !ok {
			return nil, protocol.NewError("invalid_argument", "Batch script paths cannot contain NUL, CR/LF, double quote or percent.", 2, nil)
		}
		command := script
		for _, arg := range args[1:] {
			quoted, ok := literal(arg)
			if !ok {
				return nil, protocol.NewError("invalid_argument", "Batch arguments cannot contain NUL, CR/LF, double quote or percent; use an executable directly.", 2, nil)
			}
			command += " " + quoted
		}
		system, err := windows.GetSystemDirectory()
		if err != nil {
			return nil, protocol.NewError("execution_failed", "Cannot locate the Windows command interpreter.", 126, nil)
		}
		interpreter := filepath.Join(system, "cmd.exe")
		cmd = exec.CommandContext(ctx, interpreter)
		cmd.SysProcAttr = &syscall.SysProcAttr{CmdLine: syscall.EscapeArg(interpreter) + ` /d /e:on /v:off /s /c "` + command + `"`}
	}
	cmd.Dir, cmd.Env = dir, env
	return cmd, nil
}
