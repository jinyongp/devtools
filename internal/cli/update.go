package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/jinyongp/devtools/internal/process"
	"github.com/jinyongp/devtools/internal/protocol"
	"github.com/jinyongp/devtools/scripts"
)

// Set by package-manager builds to preserve ownership of installed files.
var updateManager string

func (a *App) registerUpdate() {
	a.commands = append(a.commands, Command{
		Name: "update", Description: "Update the installed executable while preserving user data.",
		Options: []Option{{Name: "version", Description: "Release version; defaults to latest stable.", Pattern: `^0\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-[0-9A-Za-z]+([.-][0-9A-Za-z]+)*)?$`}, {Name: "source", Description: "Local release directory or HTTPS artifact directory; requires version.", MinLength: 1}},
		Output:  object(map[string]any{"action": stringSchema(), "version": stringSchema()}, "action", "version"),
		Run: func(ctx context.Context, _ IO, r Request) (any, *protocol.Error) {
			if r.Options["source"] != "" && r.Options["version"] == "" {
				return nil, argumentError("Source requires an explicit version.", "version")
			}
			executable, err := os.Executable()
			if err == nil {
				executable, err = filepath.EvalSymlinks(executable)
			}
			if err != nil || filepath.Base(executable) != "devtools" {
				return nil, protocol.NewError("update_failed", "Update requires an installed devtools executable.", 1, nil)
			}
			return updateExecutable(ctx, executable, r.Options)
		},
	})
}

func updateExecutable(ctx context.Context, executable string, options map[string]string) (any, *protocol.Error) {
	if updateManager == "homebrew" {
		return nil, protocol.NewError("package_managed", "Update this Homebrew installation with brew upgrade jinyongp/tap/devtools.", 3, map[string]any{"manager": "homebrew", "command": "brew upgrade jinyongp/tap/devtools"})
	}
	if source := options["source"]; source != "" && !strings.Contains(source, "://") {
		absolute, err := filepath.Abs(source)
		if err != nil {
			return nil, protocol.NewError("update_failed", "Cannot resolve release directory.", 1, nil)
		}
		options["source"] = absolute
	}
	args := []string{"/bin/sh", "-s", "--", "update", "--bin-dir", filepath.Dir(executable)}
	for _, name := range []string{"version", "source"} {
		if value := options[name]; value != "" {
			args = append(args, "--"+name, value)
		}
	}
	var output bytes.Buffer
	code, e := process.Execute(ctx, args, filepath.Dir(executable), os.Environ(), strings.NewReader(scripts.Installer), &output, io.Discard)
	if e != nil {
		return nil, e
	}
	if code != 0 {
		return nil, protocol.NewError("update_failed", "Cannot update executable; check release availability, installation permissions, and installer lock.", 1, nil)
	}
	var response struct {
		OK   bool `json:"ok"`
		Data struct {
			Action  string `json:"action"`
			Version string `json:"version"`
		} `json:"data"`
	}
	if json.Unmarshal(output.Bytes(), &response) != nil || !response.OK || response.Data.Action != "update" || response.Data.Version == "" {
		return nil, protocol.NewError("update_failed", "Cannot confirm update result. Run devtools version to check the installed version.", 1, nil)
	}
	return response.Data, nil
}
