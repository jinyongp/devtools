//go:build !windows

package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/jinyongp/devtools/internal/process"
	"github.com/jinyongp/devtools/internal/protocol"
	"github.com/jinyongp/devtools/scripts"
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
	var output, diagnostic updateOutput
	code, e := process.Execute(ctx, args, filepath.Dir(executable), os.Environ(), strings.NewReader(scripts.Installer), &output, &diagnostic)
	if e != nil {
		if e.Details == nil {
			e.Details = map[string]any{}
		}
		e.Details["effects"], e.Details["retry"] = "unknown", "inspect_first"
		return nil, e
	}
	if code != 0 {
		var failed struct {
			Error protocol.Error `json:"error"`
		}
		if !diagnostic.truncated && json.Unmarshal(diagnostic.Bytes(), &failed) == nil && failed.Error.Code == "install_failed" {
			failure := protocol.Public(&failed.Error, "update")
			failure.Code = "update_failed"
			failure.ExitCode = 1
			failure.Message = "Executable update failed. Inspect the installer stage and recovery state before retrying."
			return nil, failure
		}
		return nil, protocol.NewError("update_failed", "Cannot confirm the failed installer's outcome. Inspect the installed version and retained recovery state before retrying.", 1, map[string]any{"phase": "observe", "reason": "installer_result_unavailable", "effects": "unknown", "retry": "inspect_first", "facts": map[string]any{"stage": "installer"}})
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
	if output.truncated || json.Unmarshal(output.Bytes(), &response) != nil || !response.OK || response.Data.Action != "update" || response.Data.Version == "" || response.Data.Alias.Name != "dvt" || response.Data.Alias.Status == "" {
		return nil, protocol.NewError("update_failed", "Cannot confirm update result. Run devtools version to check the installed version.", 1, map[string]any{"reason": "result_unconfirmed", "effects": "unknown", "retry": "inspect_first", "facts": map[string]any{"stage": "verify_result"}})
	}
	return map[string]any{"action": response.Data.Action, "version": response.Data.Version, "alias": response.Data.Alias, "changed": true}, nil
}

type updateOutput struct {
	bytes.Buffer
	truncated bool
}

func (b *updateOutput) Write(p []byte) (int, error) {
	n := len(p)
	remaining := (64 << 10) - b.Len()
	if len(p) > remaining {
		p = p[:remaining]
		b.truncated = true
	}
	_, _ = b.Buffer.Write(p)
	return n, nil
}
