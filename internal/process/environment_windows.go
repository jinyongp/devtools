package process

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/jinyongp/devtools/internal/protocol"
)

func splitEnvironment(item string) (string, string, bool) {
	start := 0
	if strings.HasPrefix(item, "=") {
		start = 1
	}
	index := strings.IndexByte(item[start:], '=')
	if index < 0 {
		return "", "", false
	}
	index += start
	return item[:index], item[index+1:], true
}

func ValidateEnvironment(injected map[string]string) *protocol.Error {
	names := make([]string, 0, len(injected))
	for key := range injected {
		names = append(names, key)
	}
	sort.Strings(names)
	seen := map[string]string{}
	for _, key := range names {
		folded := strings.ToUpper(key)
		if previous, exists := seen[folded]; exists {
			return protocol.NewError("invalid_argument", "Injected environment names conflict on Windows.", 2, map[string]any{"keys": []string{previous, key}})
		}
		if key == "" || strings.ContainsAny(key, "=\x00") || strings.ContainsRune(injected[key], '\x00') {
			return protocol.NewError("invalid_argument", "Injected environment names or values cannot be passed to Windows.", 2, map[string]any{"keys": []string{key}})
		}
		seen[folded] = key
	}
	return nil
}

func Environment(parent []string, injected map[string]string) []string {
	type entry struct{ key, value string }
	merged := map[string]entry{}
	for _, item := range parent {
		if key, value, ok := splitEnvironment(item); ok {
			merged[strings.ToUpper(key)] = entry{key, value}
		}
	}
	keys := make([]string, 0, len(injected))
	for key := range injected {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		merged[strings.ToUpper(key)] = entry{key, injected[key]}
	}
	keys = keys[:0]
	for key := range merged {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	result := make([]string, 0, len(keys))
	for _, key := range keys {
		e := merged[key]
		result = append(result, e.key+"="+e.value)
	}
	return result
}
func selectedEnvironment(env []string, key string) string {
	for i := len(env) - 1; i >= 0; i-- {
		if name, value, ok := splitEnvironment(env[i]); ok && strings.EqualFold(name, key) {
			return value
		}
	}
	return ""
}
func IsExecutable(info os.FileInfo) bool { return info != nil && info.Mode().IsRegular() }

func LookPath(name, dir string, env []string) (string, *protocol.Error) {
	invalid := func(message string) (string, *protocol.Error) {
		return "", protocol.NewError("invalid_argument", message, 2, nil)
	}
	if name == "" || strings.ContainsRune(name, '\x00') {
		return invalid("Executable name is invalid.")
	}
	if volume := filepath.VolumeName(name); volume != "" && !filepath.IsAbs(name) {
		return invalid("Drive-relative executable paths are not supported.")
	}
	if strings.HasPrefix(name, `\\?\`) || strings.HasPrefix(name, `\\.\`) {
		return invalid("Device executable paths are not supported.")
	}
	extensions := []string{""}
	if filepath.Ext(name) == "" {
		extensions = nil
		pathext := selectedEnvironment(env, "PATHEXT")
		if pathext == "" {
			pathext = ".COM;.EXE;.BAT;.CMD"
		}
		for _, ext := range strings.Split(pathext, ";") {
			ext = strings.TrimSpace(ext)
			if ext == "" {
				continue
			}
			if !strings.HasPrefix(ext, ".") || strings.ContainsAny(ext, "\\/:\x00") {
				return invalid("PATHEXT contains an invalid extension.")
			}
			extensions = append(extensions, ext)
		}
	}
	check := func(base string) (string, bool) {
		for _, ext := range extensions {
			path := base + ext
			if info, err := os.Stat(path); err == nil && IsExecutable(info) {
				return path, true
			}
		}
		return "", false
	}
	if filepath.IsAbs(name) || strings.ContainsAny(name, `\/`) {
		if !filepath.IsAbs(name) {
			name = filepath.Join(dir, name)
		}
		if path, ok := check(name); ok {
			return path, nil
		}
	} else {
		for _, part := range filepath.SplitList(selectedEnvironment(env, "PATH")) {
			// An empty PATH component does not implicitly enable cwd execution.
			if part == "" {
				continue
			}
			if filepath.VolumeName(part) != "" && !filepath.IsAbs(part) {
				return invalid("Drive-relative PATH entries are not supported.")
			}
			if !filepath.IsAbs(part) {
				part = filepath.Join(dir, part)
			}
			if path, ok := check(filepath.Join(part, name)); ok {
				return path, nil
			}
		}
	}
	return "", protocol.NewError("command_not_found", "Executable was not found in the selected PATH/PATHEXT.", 127, nil)
}

func EnvironmentValue(values map[string]string, key string) (string, bool) {
	for name, value := range values {
		if strings.EqualFold(name, key) {
			return value, true
		}
	}
	return "", false
}
