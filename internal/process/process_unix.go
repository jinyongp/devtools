//go:build !windows

package process

import (
	"errors"
	"os"

	"path/filepath"
	"sort"
	"strings"

	"github.com/jinyongp/devtools/internal/protocol"
)

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

func splitEnvironment(item string) (string, string, bool) { return strings.Cut(item, "=") }
func sameEnvironmentName(a, b string) bool                { return a == b }

func CompleteEnvironment(environment []string) []string { return append([]string{}, environment...) }

// LookPath resolves an executable using the command's directory and environment.
func LookPath(name, dir string, env []string) (string, *protocol.Error) {
	reason := "not_found"
	check := func(path string) bool {
		info, err := os.Stat(path)
		if errors.Is(err, os.ErrPermission) {
			reason = "permission_denied"
		}
		if err == nil && (!info.Mode().IsRegular() || info.Mode().Perm()&0111 == 0) {
			reason = "not_executable"
		}
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
	return "", protocol.NewError("command_not_found", "Executable was not found or usable in the selected PATH.", 127, map[string]any{"reason": reason})
}

func ValidateEnvironment(injected map[string]string) *protocol.Error { return nil }
func IsExecutable(info os.FileInfo) bool {
	return info != nil && info.Mode().IsRegular() && info.Mode().Perm()&0111 != 0
}

func EnvironmentValue(values map[string]string, key string) (string, bool) {
	value, ok := values[key]
	return value, ok
}
