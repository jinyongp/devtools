package hosts

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/jinyongp/devtools/internal/platformfs"
	"github.com/jinyongp/devtools/internal/protocol"
	"golang.org/x/sys/windows"
)

func windowsHostsFailure(code, stage string, err error) *protocol.Error {
	problem := failure(code)
	problem.Details = map[string]any{"stage": stage}
	var number windows.Errno
	if errors.As(err, &number) {
		problem.Details["win32_code"] = uint32(number)
	}
	if errors.Is(err, platformfs.ErrUnsafe) {
		problem.Details["reason"] = "unsafe_metadata"
	}
	return problem
}

func (s System) path() string {
	if s.Path != "" {
		return s.Path
	}
	directory, err := windows.GetWindowsDirectory()
	if err != nil {
		return ""
	}
	return filepath.Join(directory, "System32", "drivers", "etc", "hosts")
}
func (s System) Read() ([]byte, *protocol.Error) {
	file, err := platformfs.OpenPublicNoFollow(s.path(), false)
	if err != nil {
		return nil, failure("hosts_unavailable")
	}
	defer file.Close()
	body, err := io.ReadAll(io.LimitReader(file, maxHostsSize+1))
	if err != nil || len(body) > maxHostsSize {
		return nil, failure("hosts_unavailable")
	}
	return body, nil
}

func (s System) observe() ([]byte, bool, *protocol.Error) {
	body, e := s.Read()
	return body, false, e
}
func (s System) Apply(ctx context.Context, request Request) *protocol.Error {
	if ctx.Err() != nil {
		return failure("canceled")
	}
	release, err := enableHostsMetadataPrivileges()
	if err != nil {
		return windowsHostsFailure("permission_required", "metadata_privileges", err)
	}
	defer release()
	file, err := platformfs.OpenPublicNoFollow(s.path(), true)
	if err != nil {
		if errors.Is(err, os.ErrPermission) || errors.Is(err, windows.ERROR_ACCESS_DENIED) {
			return windowsHostsFailure("permission_required", "open", err)
		}
		return windowsHostsFailure("hosts_unavailable", "open", err)
	}
	defer file.Close()
	handle := windows.Handle(file.Fd())
	// A byte beyond the read limit serializes helpers without blocking readers.
	byteLock := windows.Overlapped{Offset: 2 * maxHostsSize}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	tick := time.NewTicker(15 * time.Millisecond)
	defer tick.Stop()
	for {
		if ctx.Err() != nil {
			return failure("canceled")
		}
		err = windows.LockFileEx(handle, windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY, 0, 1, 0, &byteLock)
		if err == nil {
			break
		}
		if !errors.Is(err, windows.ERROR_LOCK_VIOLATION) {
			return windowsHostsFailure("hosts_unavailable", "lock", err)
		}
		select {
		case <-ctx.Done():
			return failure("canceled")
		case <-tick.C:
		}
	}
	defer windows.UnlockFileEx(handle, 0, 1, 0, &byteLock)
	if err = sameHostsFile(handle, file.Name()); err != nil {
		return windowsHostsFailure("stale_preview", "identity", err)
	}
	before, err := io.ReadAll(io.LimitReader(file, maxHostsSize+1))
	if err != nil || len(before) > maxHostsSize {
		return failure("hosts_unavailable")
	}
	if digest(before) != request.Expected {
		return failure("stale_preview")
	}
	after, failureResult := Render(before, request.Mappings)
	if failureResult != nil {
		return failureResult
	}
	if bytes.Equal(before, after) {
		return nil
	}
	if ctx.Err() != nil {
		return failure("canceled")
	}
	if err = replaceHostsFile(ctx, file, after); err != nil {
		if ctx.Err() != nil {
			return failure("canceled")
		}
		if errors.Is(err, os.ErrPermission) {
			return windowsHostsFailure("permission_required", "publication", err)
		}
		return windowsHostsFailure("hosts_unavailable", "publication", err)
	}
	return nil
}
