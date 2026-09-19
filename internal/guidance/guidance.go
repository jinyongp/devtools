// Package guidance resolves portable, path-scoped agent instruction sources.
package guidance

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/jinyongp/devtools/internal/location"
)

const (
	Filename       = "AGENTS.md"
	MaxSources     = 32
	MaxSourceBytes = 512 << 10
	MaxTotalBytes  = 2 << 20
)

type Source struct {
	Path     string `json:"path"`
	Scope    string `json:"scope"`
	Revision string `json:"revision"`
	Bytes    int    `json:"bytes"`
	Content  string `json:"content"`
}

type Diagnostic struct {
	Path    string `json:"path"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

type Result struct {
	Target      string       `json:"target"`
	TargetDir   string       `json:"target_dir"`
	Revision    string       `json:"revision"`
	Complete    bool         `json:"complete"`
	Sources     []Source     `json:"sources"`
	Diagnostics []Diagnostic `json:"diagnostics"`
	TotalBytes  int          `json:"total_bytes"`
}

type BoundaryError struct {
	Message string
}

func (e *BoundaryError) Error() string { return e.Message }

func within(root, path string) (string, bool) {
	relative, err := filepath.Rel(root, path)
	if err != nil {
		return "", false
	}
	relative = filepath.Clean(relative)
	if relative == "." {
		return ".", true
	}
	if relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) || filepath.IsAbs(relative) {
		return "", false
	}
	return filepath.ToSlash(relative), true
}

func targetDirectory(canonical string) (string, error) {
	info, err := os.Stat(canonical)
	if err == nil {
		if info.IsDir() {
			return canonical, nil
		}
		if info.Mode().IsRegular() {
			return filepath.Dir(canonical), nil
		}
		return "", errors.New("guidance target must be a regular file or directory")
	}
	if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	return filepath.Dir(canonical), nil
}

func directories(root, target string) ([]string, error) {
	relative, ok := within(root, target)
	if !ok {
		return nil, &BoundaryError{Message: "guidance target is outside the project root"}
	}
	items := []string{root}
	if relative == "." {
		return items, nil
	}
	current := root
	for _, part := range strings.Split(filepath.FromSlash(relative), string(filepath.Separator)) {
		current = filepath.Join(current, part)
		items = append(items, current)
	}
	return items, nil
}

func readSource(projectRoot, directory string) (Source, *Diagnostic, error) {
	logical := filepath.Join(directory, Filename)
	info, err := os.Lstat(logical)
	if errors.Is(err, os.ErrNotExist) {
		return Source{}, nil, nil
	}
	if err != nil {
		relative, _ := within(projectRoot, logical)
		return Source{}, &Diagnostic{Path: relative, Code: "unreadable", Message: "Cannot inspect AGENTS.md."}, nil
	}

	readPath := logical
	if info.Mode()&os.ModeSymlink != 0 {
		resolved, resolveErr := filepath.EvalSymlinks(logical)
		if resolveErr != nil {
			return Source{}, nil, &BoundaryError{Message: "AGENTS.md symlink cannot be resolved safely"}
		}
		if _, ok := within(projectRoot, resolved); !ok {
			return Source{}, nil, &BoundaryError{Message: "AGENTS.md symlink escapes the project root"}
		}
		resolvedInfo, statErr := os.Stat(resolved)
		if statErr != nil || !resolvedInfo.Mode().IsRegular() {
			return Source{}, nil, &BoundaryError{Message: "AGENTS.md symlink must resolve to a regular file"}
		}
		readPath = resolved
	} else if !info.Mode().IsRegular() {
		return Source{}, nil, &BoundaryError{Message: "AGENTS.md must be a regular file"}
	}

	file, err := os.Open(readPath)
	if err != nil {
		relative, _ := within(projectRoot, logical)
		return Source{}, &Diagnostic{Path: relative, Code: "unreadable", Message: "Cannot read AGENTS.md."}, nil
	}
	defer file.Close()

	data, err := io.ReadAll(io.LimitReader(file, MaxSourceBytes+1))
	relative, _ := within(projectRoot, logical)
	if err != nil {
		return Source{}, &Diagnostic{Path: relative, Code: "unreadable", Message: "Cannot read AGENTS.md."}, nil
	}
	if len(data) > MaxSourceBytes {
		return Source{}, &Diagnostic{Path: relative, Code: "oversized", Message: "AGENTS.md exceeds the source size limit."}, nil
	}
	if !utf8.Valid(data) {
		return Source{}, &Diagnostic{Path: relative, Code: "invalid_utf8", Message: "AGENTS.md must be valid UTF-8."}, nil
	}
	sum := sha256.Sum256(data)
	scope, _ := within(projectRoot, directory)
	return Source{
		Path: relative, Scope: scope, Revision: hex.EncodeToString(sum[:]),
		Bytes: len(data), Content: string(data),
	}, nil, nil
}

// Resolve returns the broad-to-specific AGENTS.md chain for an actual work target.
// It performs no semantic merge and never searches above projectRoot.
func Resolve(projectRoot, target string) (Result, error) {
	root, err := location.ExistingDirectory(projectRoot)
	if err != nil {
		return Result{}, err
	}
	if target == "" {
		target = "."
	}
	candidate := target
	if !filepath.IsAbs(candidate) {
		candidate = filepath.Join(root, candidate)
	}
	canonicalTarget, err := location.Canonical(candidate)
	if err != nil {
		return Result{}, err
	}
	if _, ok := within(root, canonicalTarget); !ok {
		return Result{}, &BoundaryError{Message: "guidance target is outside the project root"}
	}
	dir, err := targetDirectory(canonicalTarget)
	if err != nil {
		return Result{}, err
	}
	if _, ok := within(root, dir); !ok {
		return Result{}, &BoundaryError{Message: "guidance target directory is outside the project root"}
	}
	chain, err := directories(root, dir)
	if err != nil {
		return Result{}, err
	}
	result := Result{Complete: true, Sources: []Source{}, Diagnostics: []Diagnostic{}}
	result.Target, _ = within(root, canonicalTarget)
	result.TargetDir, _ = within(root, dir)
	hasher := sha256.New()
	_, _ = hasher.Write([]byte(result.TargetDir))
	_, _ = hasher.Write([]byte{0})

	for _, directory := range chain {
		source, diagnostic, readErr := readSource(root, directory)
		if readErr != nil {
			return Result{}, readErr
		}
		if diagnostic != nil {
			result.Complete = false
			result.Diagnostics = append(result.Diagnostics, *diagnostic)
			continue
		}
		if source.Path == "" {
			continue
		}
		if len(result.Sources) >= MaxSources {
			result.Complete = false
			result.Diagnostics = append(result.Diagnostics, Diagnostic{
				Path: source.Path, Code: "too_many_sources", Message: "AGENTS.md chain exceeds the source count limit.",
			})
			continue
		}
		if result.TotalBytes+source.Bytes > MaxTotalBytes {
			result.Complete = false
			result.Diagnostics = append(result.Diagnostics, Diagnostic{
				Path: source.Path, Code: "total_oversized", Message: "AGENTS.md chain exceeds the total content limit.",
			})
			continue
		}
		result.TotalBytes += source.Bytes
		result.Sources = append(result.Sources, source)
		_, _ = hasher.Write([]byte(source.Scope))
		_, _ = hasher.Write([]byte{0})
		_, _ = hasher.Write([]byte(source.Path))
		_, _ = hasher.Write([]byte{0})
		_, _ = hasher.Write([]byte(source.Revision))
		_, _ = hasher.Write([]byte{0})
	}
	result.Revision = hex.EncodeToString(hasher.Sum(nil))
	return result, nil
}
