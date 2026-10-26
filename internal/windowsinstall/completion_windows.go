package windowsinstall

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/jinyongp/devtools/internal/platformfs"
	"golang.org/x/sys/windows"
)

func registerPowerShellCompletion(ctx context.Context, executable string, alias bool) error {
	local, err := windows.KnownFolderPath(windows.FOLDERID_LocalAppData, windows.KF_FLAG_DONT_VERIFY)
	if err != nil {
		return err
	}
	documents, err := windows.KnownFolderPath(windows.FOLDERID_Documents, windows.KF_FLAG_DONT_VERIFY)
	if err != nil {
		return err
	}
	childCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	command := exec.CommandContext(childCtx, executable, "completion", "powershell")
	command.Env = installerEnvironment()
	var output completionOutput
	command.Stdout, command.Stderr = &output, io.Discard
	if err := command.Run(); err != nil {
		return err
	}
	if !bytes.Contains(output.body, []byte("Register-ArgumentCompleter -Native")) {
		return errors.New("installed executable did not generate PowerShell completion")
	}
	if !alias {
		output.body = bytes.ReplaceAll(output.body, []byte("-CommandName devtools,dvt,devtools.exe,dvt.exe"), []byte("-CommandName devtools,devtools.exe"))
	}
	return publishPowerShellCompletion(ctx, filepath.Join(local, "devtools", "completion"), documents, output.body)
}

// A separate shared lock serializes installation directories writing the same
// user's script/profiles, after the executable transaction commits.
func publishPowerShellCompletion(ctx context.Context, directory, documents string, body []byte) error {
	root, err := platformfs.OpenRoot(directory, true)
	if err != nil {
		return err
	}
	defer root.Close()
	lock, err := root.OpenExclusive(".registration.lock", true, false)
	if err != nil {
		return err
	}
	defer lock.Close()
	script := append([]byte{0xef, 0xbb, 0xbf}, body...)
	if err := root.WriteFile(ctx, "powershell.ps1", script, true); err != nil {
		return err
	}
	for _, shell := range []string{"WindowsPowerShell", "PowerShell"} {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := appendCompletionProfile(filepath.Join(documents, shell, "profile.ps1")); err != nil {
			return err
		}
	}
	return nil
}

// Child wire formats must not depend on the caller's presentation preference.
// Unsetting rather than passing new flags keeps older release candidates usable.
func installerEnvironment() []string {
	var result []string
	for _, entry := range os.Environ() {
		if !strings.HasPrefix(strings.ToUpper(entry), "DEVTOOLS_FORMAT=") {
			result = append(result, entry)
		}
	}
	return result
}

type completionOutput struct{ body []byte }

func (w *completionOutput) Write(body []byte) (int, error) {
	if len(w.body)+len(body) > 1024*1024 {
		return 0, errors.New("PowerShell completion script exceeds size limit")
	}
	w.body = append(w.body, body...)
	return len(body), nil
}

func appendCompletionProfile(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	name, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return err
	}
	// Pin the actual profile while appending, including user-configured redirects
	// such as OneDrive/Documents. Preserve ACLs and deny concurrent writes/deletion.
	handle, err := windows.CreateFile(name, windows.GENERIC_READ|windows.GENERIC_WRITE, windows.FILE_SHARE_READ, nil, windows.OPEN_ALWAYS, windows.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		return err
	}
	file := os.NewFile(uintptr(handle), path)
	defer file.Close()
	var info windows.ByHandleFileInformation
	if err := windows.GetFileInformationByHandle(handle, &info); err != nil {
		return err
	}
	if info.FileAttributes&windows.FILE_ATTRIBUTE_DIRECTORY != 0 {
		return errors.New("PowerShell profile must be a regular file")
	}
	body, err := io.ReadAll(io.LimitReader(file, 1024*1024+1))
	if err != nil {
		return err
	}
	if len(body) > 1024*1024 {
		return errors.New("PowerShell profile exceeds size limit")
	}
	suffix, err := completionProfileAppend(body)
	if err != nil || len(suffix) == 0 {
		return err
	}
	if _, err := file.WriteAt(suffix, int64(len(body))); err != nil {
		return errors.Join(err, file.Truncate(int64(len(body))))
	}
	return file.Sync()
}
