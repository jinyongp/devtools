package windowsinstall

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/jinyongp/devtools/internal/location"
	"github.com/jinyongp/devtools/internal/platformfs"
	"golang.org/x/sys/windows"
)

func DefaultBinDir() (string, error) {
	base, err := windows.KnownFolderPath(windows.FOLDERID_LocalAppData, windows.KF_FLAG_DONT_VERIFY)
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "Programs", "devtools", "bin"), nil
}
func Bootstrap(ctx context.Context, options Options) (*Result, error) {
	if options.Action != "install" || options.Source != "" || !versionToken.MatchString(options.Version) {
		return nil, errors.New("invalid bootstrap installation request")
	}
	executable, err := os.Executable()
	if err != nil {
		return nil, err
	}
	return apply(ctx, options, "", func(ctx context.Context, arch string) ([]byte, string, error) {
		file, err := os.Open(executable)
		if err != nil {
			return nil, "", err
		}
		defer file.Close()
		body, err := readBounded(file, maxImage)
		return body, options.Version, err
	})
}
func Update(ctx context.Context, executable, version, source string) (*Result, error) {
	return apply(ctx, Options{Action: "update", BinDir: filepath.Dir(executable), Version: version, Source: source}, executable, func(ctx context.Context, arch string) ([]byte, string, error) {
		return release(ctx, version, source, arch)
	})
}
func apply(ctx context.Context, options Options, invoked string, load func(context.Context, string) ([]byte, string, error)) (out *Result, failure error) {
	stage, effects := "input", "none"
	defer func() {
		if failure != nil {
			failure = &Failure{Stage: stage, Effects: effects, Cause: failure}
		}
	}()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if os.Getenv("DEVTOOLS_ISOLATED_ROOT") != "" {
		return nil, errors.New("installation changes are unavailable in isolated mode")
	}
	if options.Action != "install" && options.Action != "update" {
		return nil, errors.New("invalid install action")
	}
	if options.BinDir == "" {
		var err error
		options.BinDir, err = DefaultBinDir()
		if err != nil {
			return nil, err
		}
	}
	directory, err := filepath.Abs(options.BinDir)
	if err != nil {
		return nil, err
	}
	directory, err = platformfs.ValidatePrivatePath(directory)
	if err != nil {
		return nil, err
	}
	stage = "installation_directory"
	root, err := platformfs.OpenRoot(directory, options.Action == "install")
	if err != nil {
		return nil, err
	}
	defer root.Close()
	stage = "lock"
	lock, err := root.OpenExclusive(".devtools-install.lock", true, false)
	if err != nil {
		return nil, err
	}
	defer lock.Close()
	info, err := lock.Stat()
	if err != nil {
		return nil, err
	}
	if info.Size() != 0 {
		return nil, errors.New("install lock must remain empty")
	}
	e := engine{root: root, directory: directory, smoke: smoke}
	stage = "recovery"
	if err = e.recover(ctx); err != nil {
		effects = "unknown"
		return nil, err
	}
	stage = "installation_ownership"
	var before manifest
	exists, err := e.readJSON(manifestName, &before)
	if err != nil {
		return nil, err
	}
	if exists && !validManifest(&before) {
		return nil, errors.New("invalid installation manifest")
	}
	var previous *manifest
	if !exists {
		if options.Action == "update" {
			return nil, errors.New("update requires a standalone installation manifest")
		}
		if _, err := os.Lstat(filepath.Join(directory, "devtools.exe")); err == nil {
			return nil, errors.New("foreign main executable already exists")
		} else if !errors.Is(err, os.ErrNotExist) {
			return nil, err
		}
	} else {
		if err = e.require("devtools.exe", before.Main); err != nil {
			return nil, err
		}
		if before.Alias != nil {
			if err = e.require("dvt.exe", *before.Alias); err != nil {
				return nil, err
			}
		}
		// A bootstrap runs the verified staged candidate. Explicit CLI updates
		// must still originate from an owned installed executable.
		if options.Action == "update" {
			base := strings.ToLower(filepath.Base(invoked))
			if base != "devtools.exe" && base != "dvt.exe" {
				return nil, errors.New("update requires an owned installed executable")
			}
			if base == "dvt.exe" && before.Alias == nil {
				return nil, errors.New("alias is not owned by this installation")
			}
			actual, err := location.Canonical(invoked)
			if err != nil {
				return nil, err
			}
			expected, err := location.Canonical(filepath.Join(directory, base))
			if err != nil || actual != expected {
				return nil, errors.New("update invocation identity differs from installation")
			}
		}
		options.Action = "update"
		previous = &before
		if err = e.cleanupRetired(ctx, previous); err != nil {
			return nil, err
		}
	}
	stage = "native_architecture"
	arch, err := NativeArchitecture()
	if err != nil {
		return nil, err
	}
	stage = "artifact_fetch"
	body, version, err := load(ctx, arch)
	if err != nil {
		return nil, err
	}
	stage = "image_identity"
	want, err := inspectImage(body, version, arch)
	if err != nil {
		return nil, err
	}
	stage, effects = "publish", "unknown"
	result, err := e.transact(ctx, options, body, want, previous)
	if err != nil {
		return nil, err
	}
	stage, effects = "powershell_completion", "partial"
	if err := registerPowerShellCompletion(ctx, filepath.Join(directory, "devtools.exe"), result.Alias.Status != "skipped_conflict"); err != nil {
		return nil, err
	}
	return result, nil
}

// DecodeBootstrap never accepts unknown fields or a second JSON value.
func DecodeBootstrap(reader io.Reader) (Options, error) {
	var options Options
	body, err := readBounded(reader, 4096)
	if err != nil {
		return options, err
	}
	err = decodeStrict(body, &options)
	return options, err
}
