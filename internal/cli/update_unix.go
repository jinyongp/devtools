//go:build !windows

package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/jinyongp/devtools/internal/process"
	"github.com/jinyongp/devtools/internal/protocol"
	"github.com/jinyongp/devtools/scripts"
	"golang.org/x/term"
	"io"
	"os"
	"path/filepath"
	"strings"
)

func installedExecutableName(name string) bool { return name == "devtools" }
func updatePlatform(ctx context.Context, executable string, options map[string]string, streams ...IO) (any, *protocol.Error) {
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
	diagnostic := io.Writer(io.Discard)
	if len(streams) > 0 {
		if f, ok := streams[0].Err.(*os.File); ok && term.IsTerminal(int(f.Fd())) {
			diagnostic = f
		}
	}
	code, e := process.Execute(ctx, args, filepath.Dir(executable), os.Environ(), strings.NewReader(scripts.Installer), &output, diagnostic)
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
			Alias   struct {
				Name   string `json:"name"`
				Status string `json:"status"`
			} `json:"alias"`
		} `json:"data"`
	}
	if json.Unmarshal(output.Bytes(), &response) != nil || !response.OK || response.Data.Action != "update" || response.Data.Version == "" || response.Data.Alias.Name != "dvt" || response.Data.Alias.Status == "" {
		return nil, protocol.NewError("update_failed", "Cannot confirm update result. Run devtools version to check the installed version.", 1, nil)
	}
	return map[string]any{"action": response.Data.Action, "version": response.Data.Version, "alias": response.Data.Alias, "changed": true}, nil
}
