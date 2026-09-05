// Package process executes commands with explicit environment and stream inheritance.
package process

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/jinyongp/devtools/internal/protocol"
)

type SignalError struct{ Signal syscall.Signal }

func (s SignalError) Error() string { return "execution interrupted by signal" }

func Environment(parent []string, injected map[string]string) []string {
	merged := map[string]string{}
	for _, item := range parent {
		if key, value, ok := strings.Cut(item, "="); ok {
			merged[key] = value
		}
	}
	for key, value := range injected {
		merged[key] = value
	}
	keys := make([]string, 0, len(merged))
	for key := range merged {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	result := make([]string, 0, len(keys))
	for _, key := range keys {
		result = append(result, key+"="+merged[key])
	}
	return result
}

func executable(name, dir string, env []string) (string, *protocol.Error) {
	check := func(path string) bool {
		info, err := os.Stat(path)
		return err == nil && info.Mode().IsRegular() && info.Mode().Perm()&0111 != 0
	}
	if strings.ContainsRune(name, filepath.Separator) {
		if !filepath.IsAbs(name) {
			name = filepath.Join(dir, name)
		}
		// Start reports missing paths and permission failures without printing args.
		return name, nil
	}
	search := ""
	for _, item := range env {
		if strings.HasPrefix(item, "PATH=") {
			search = strings.TrimPrefix(item, "PATH=")
		}
	}
	for _, part := range filepath.SplitList(search) {
		if !filepath.IsAbs(part) {
			part = filepath.Join(dir, part)
		}
		candidate := filepath.Join(part, name)
		if check(candidate) {
			return candidate, nil
		}
	}
	return "", protocol.NewError("command_not_found", "Executable was not found in the selected PATH.", 127, nil)
}

func Execute(ctx context.Context, args []string, dir string, env []string, in io.Reader, out, diagnostic io.Writer) (int, *protocol.Error) {
	if len(args) == 0 || args[0] == "" {
		return 0, protocol.NewError("invalid_argument", "Specify a command to execute.", 2, nil)
	}
	if ctx.Err() != nil {
		return 0, protocol.NewError("canceled", "Execution canceled.", 130, nil)
	}
	absolute, err := filepath.Abs(dir)
	if err != nil {
		return 0, protocol.NewError("io_error", "Cannot resolve execution directory.", 1, nil)
	}
	path, lookupErr := executable(args[0], absolute, env)
	if lookupErr != nil {
		return 0, lookupErr
	}
	command := exec.Command(path, args[1:]...)
	command.Args[0] = args[0]
	command.Dir, command.Env = absolute, env
	command.Stdin, command.Stdout, command.Stderr = in, out, diagnostic
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	command.WaitDelay = 3 * time.Second
	if err := command.Start(); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return 0, protocol.NewError("command_not_found", "Executable or execution directory was not found.", 127, nil)
		}
		return 0, protocol.NewError("execution_failed", "Cannot start the requested command.", 126, nil)
	}
	done := make(chan struct{})
	watcherDone := make(chan struct{})
	go func() {
		defer close(watcherDone)
		select {
		case <-done:
			return
		case <-ctx.Done():
		}
		select {
		case <-done:
			return
		default:
		}
		signal := syscall.SIGTERM
		var cause SignalError
		if errors.As(context.Cause(ctx), &cause) {
			signal = cause.Signal
		}
		_ = syscall.Kill(-command.Process.Pid, signal)
		timer := time.NewTimer(3 * time.Second)
		defer timer.Stop()
		select {
		case <-done:
			// Cancellation also owns descendants that outlive the direct child.
			_ = syscall.Kill(-command.Process.Pid, syscall.SIGKILL)
		case <-timer.C:
			_ = syscall.Kill(-command.Process.Pid, syscall.SIGKILL)
		}
	}()
	err = command.Wait()
	close(done)
	<-watcherDone
	if err == nil {
		return 0, nil
	}
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		if status, ok := exit.Sys().(syscall.WaitStatus); ok && status.Signaled() {
			return 128 + int(status.Signal()), nil
		}
		return exit.ExitCode(), nil
	}
	return 0, protocol.NewError("io_error", "Command stream handling failed.", 1, nil)
}
