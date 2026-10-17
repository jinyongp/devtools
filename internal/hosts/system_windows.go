package hosts

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"

	"github.com/jinyongp/devtools/internal/platformfs"
	"github.com/jinyongp/devtools/internal/protocol"
	"golang.org/x/sys/windows"
)

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
	body, err := io.ReadAll(io.LimitReader(file, 1<<20+1))
	if err != nil || len(body) > 1<<20 {
		return nil, failure("hosts_unavailable")
	}
	return body, nil
}
func (s System) Apply(ctx context.Context, request Request) *protocol.Error {
	if ctx.Err() != nil {
		return failure("canceled")
	}
	release, err := enableHostsMetadataPrivileges()
	if err != nil {
		return failure("permission_required")
	}
	defer release()
	file, err := platformfs.OpenPublicNoFollow(s.path(), true)
	if err != nil {
		if errors.Is(err, os.ErrPermission) || errors.Is(err, windows.ERROR_ACCESS_DENIED) {
			return failure("permission_required")
		}
		return failure("hosts_unavailable")
	}
	defer file.Close()
	before, err := io.ReadAll(io.LimitReader(file, 1<<20+1))
	if err != nil || len(before) > 1<<20 {
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
			return failure("permission_required")
		}
		return failure("hosts_unavailable")
	}
	return nil
}
