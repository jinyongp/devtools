// Package skills implements portable Agent Skills discovery and local registration.
package skills

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"syscall"
	"unicode/utf8"

	"gopkg.in/yaml.v3"
)

const (
	MaxSkills        = 256
	MaxFiles         = 512
	MaxSkillBytes    = 512 << 10
	MaxResourceBytes = 2 << 20
	MaxTotalBytes    = 16 << 20
)

type Scope string

const (
	Project Scope = "project"
	User    Scope = "user"
)

var (
	skillName = regexp.MustCompile("^[a-z0-9]+(?:-[a-z0-9]+)*$")
	ErrExists = errors.New("skill already exists")
)

type Resource struct {
	Path       string `json:"path"`
	Size       int64  `json:"size"`
	SHA256     string `json:"sha256"`
	Executable bool   `json:"executable"`
}

type Skill struct {
	Name          string            `json:"name"`
	Description   string            `json:"description"`
	Scope         Scope             `json:"scope"`
	Root          string            `json:"root"`
	Revision      string            `json:"revision"`
	License       string            `json:"license,omitempty"`
	Compatibility string            `json:"compatibility,omitempty"`
	Metadata      map[string]string `json:"metadata,omitempty"`
	AllowedTools  string            `json:"allowed_tools,omitempty"`
	ResourceCount int               `json:"resource_count"`
	TotalBytes    int64             `json:"total_bytes"`
	Content       string            `json:"content,omitempty"`
	Resources     []Resource        `json:"resources,omitempty"`
}

type Diagnostic struct {
	Scope   Scope  `json:"scope"`
	Path    string `json:"path"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

type Shadow struct {
	Name          string `json:"name"`
	SelectedScope Scope  `json:"selected_scope"`
	SelectedRoot  string `json:"selected_root"`
	ShadowedScope Scope  `json:"shadowed_scope"`
	ShadowedRoot  string `json:"shadowed_root"`
}

type Catalog struct {
	Items       []Skill      `json:"items"`
	Diagnostics []Diagnostic `json:"diagnostics"`
	Shadowed    []Shadow     `json:"shadowed"`
}

type ValidationError struct {
	Code    string
	Path    string
	Message string
}

func (e *ValidationError) Error() string { return e.Message }

type frontmatter struct {
	Name          string
	Description   string
	License       string
	Compatibility string
	Metadata      map[string]string
	AllowedTools  string
}

type treeFile struct {
	path       string
	data       []byte
	executable bool
}

func scopeRoot(anchor string) (string, error) {
	if !filepath.IsAbs(anchor) {
		return "", errors.New("skill scope root must be absolute")
	}
	resolved, err := filepath.EvalSymlinks(anchor)
	if err != nil {
		return "", err
	}
	info, err := os.Lstat(resolved)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return "", errors.New("skill scope root must be a directory")
	}
	agents := filepath.Join(resolved, ".agents")
	root := filepath.Join(agents, "skills")
	for _, path := range []string{agents, root} {
		info, err := os.Lstat(path)
		if errors.Is(err, os.ErrNotExist) {
			return root, nil
		}
		if err != nil {
			return "", err
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return "", errors.New("skill registry path is not a regular directory")
		}
	}
	return root, nil
}

func parseFrontmatter(path string, data []byte) (frontmatter, error) {
	if len(data) > MaxSkillBytes {
		return frontmatter{}, &ValidationError{Code: "skill_too_large", Path: path, Message: "SKILL.md exceeds the size limit"}
	}
	if !utf8.Valid(data) {
		return frontmatter{}, &ValidationError{Code: "invalid_utf8", Path: path, Message: "SKILL.md must be valid UTF-8"}
	}
	lines := bytes.Split(data, []byte("\n"))
	if len(lines) < 3 || string(bytes.TrimSuffix(lines[0], []byte("\r"))) != "---" {
		return frontmatter{}, &ValidationError{Code: "missing_frontmatter", Path: path, Message: "SKILL.md must start with YAML frontmatter"}
	}
	end := -1
	for i := 1; i < len(lines); i++ {
		if string(bytes.TrimSuffix(lines[i], []byte("\r"))) == "---" {
			end = i
			break
		}
	}
	if end < 0 {
		return frontmatter{}, &ValidationError{Code: "missing_frontmatter", Path: path, Message: "SKILL.md frontmatter is not closed"}
	}
	rawYAML := bytes.Join(lines[1:end], []byte("\n"))
	var node yaml.Node
	if err := yaml.Unmarshal(rawYAML, &node); err != nil {
		return frontmatter{}, &ValidationError{Code: "invalid_yaml", Path: path, Message: "SKILL.md frontmatter is invalid YAML"}
	}
	if len(node.Content) != 1 || node.Content[0].Kind != yaml.MappingNode {
		return frontmatter{}, &ValidationError{Code: "invalid_yaml", Path: path, Message: "SKILL.md frontmatter must be a mapping"}
	}
	mapping := node.Content[0]
	seen := map[string]bool{}
	for i := 0; i+1 < len(mapping.Content); i += 2 {
		key := mapping.Content[i].Value
		if seen[key] {
			return frontmatter{}, &ValidationError{Code: "duplicate_field", Path: path, Message: "SKILL.md frontmatter contains a duplicate field"}
		}
		seen[key] = true
	}
	var values map[string]any
	if err := mapping.Decode(&values); err != nil {
		return frontmatter{}, &ValidationError{Code: "invalid_yaml", Path: path, Message: "SKILL.md frontmatter cannot be decoded"}
	}
	readString := func(key string, required bool) (string, error) {
		value, exists := values[key]
		if !exists {
			if required {
				return "", &ValidationError{Code: "missing_field", Path: path, Message: "SKILL.md frontmatter requires " + key}
			}
			return "", nil
		}
		text, ok := value.(string)
		if !ok {
			return "", &ValidationError{Code: "invalid_field", Path: path, Message: "SKILL.md " + key + " must be a string"}
		}
		return text, nil
	}
	name, err := readString("name", true)
	if err != nil {
		return frontmatter{}, err
	}
	description, err := readString("description", true)
	if err != nil {
		return frontmatter{}, err
	}
	license, err := readString("license", false)
	if err != nil {
		return frontmatter{}, err
	}
	compatibility, err := readString("compatibility", false)
	if err != nil {
		return frontmatter{}, err
	}
	allowedTools, err := readString("allowed-tools", false)
	if err != nil {
		return frontmatter{}, err
	}
	if len(name) == 0 || len(name) > 64 || !skillName.MatchString(name) {
		return frontmatter{}, &ValidationError{Code: "invalid_name", Path: path, Message: "Skill name must use lowercase letters, numbers, and single hyphens"}
	}
	if strings.TrimSpace(description) == "" || utf8.RuneCountInString(description) > 1024 {
		return frontmatter{}, &ValidationError{Code: "invalid_description", Path: path, Message: "Skill description must be non-empty and at most 1024 characters"}
	}
	if utf8.RuneCountInString(compatibility) > 500 {
		return frontmatter{}, &ValidationError{Code: "invalid_compatibility", Path: path, Message: "Skill compatibility must be at most 500 characters"}
	}
	metadata := map[string]string{}
	if raw, exists := values["metadata"]; exists {
		object, ok := raw.(map[string]any)
		if !ok {
			return frontmatter{}, &ValidationError{Code: "invalid_metadata", Path: path, Message: "Skill metadata must be a string mapping"}
		}
		for key, value := range object {
			text, ok := value.(string)
			if !ok {
				return frontmatter{}, &ValidationError{Code: "invalid_metadata", Path: path, Message: "Skill metadata values must be strings"}
			}
			metadata[key] = text
		}
	}
	if len(metadata) == 0 {
		metadata = nil
	}
	return frontmatter{Name: name, Description: description, License: license, Compatibility: compatibility, Metadata: metadata, AllowedTools: allowedTools}, nil
}

func readFile(path string, limit int64) ([]byte, os.FileMode, error) {
	file, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, 0, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, 0, err
	}
	if !info.Mode().IsRegular() || info.Size() > limit {
		return nil, 0, errors.New("unsupported or oversized skill file")
	}
	data, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil {
		return nil, 0, err
	}
	if int64(len(data)) > limit {
		return nil, 0, errors.New("skill file exceeds size limit")
	}
	return data, info.Mode(), nil
}

func inspectTree(root string, scope Scope, content bool) (Skill, []treeFile, error) {
	absolute, err := filepath.Abs(root)
	if err != nil {
		return Skill{}, nil, err
	}
	info, err := os.Lstat(absolute)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return Skill{}, nil, &ValidationError{Code: "invalid_root", Path: absolute, Message: "Skill root must be a regular directory"}
	}
	files := []treeFile{}
	total := int64(0)
	err = filepath.WalkDir(absolute, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if path == absolute {
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return &ValidationError{Code: "unsupported_symlink", Path: path, Message: "Skill trees cannot contain symlinks"}
		}
		if entry.IsDir() {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return &ValidationError{Code: "unsupported_file", Path: path, Message: "Skill trees can contain only regular files and directories"}
		}
		if len(files) >= MaxFiles {
			return &ValidationError{Code: "too_many_files", Path: absolute, Message: "Skill tree contains too many files"}
		}
		relative, err := filepath.Rel(absolute, path)
		if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			return &ValidationError{Code: "path_escape", Path: path, Message: "Skill file escapes its root"}
		}
		limit := int64(MaxResourceBytes)
		if filepath.ToSlash(relative) == "SKILL.md" {
			limit = MaxSkillBytes
		}
		data, mode, err := readFile(path, limit)
		if err != nil {
			return &ValidationError{Code: "invalid_file", Path: path, Message: err.Error()}
		}
		total += int64(len(data))
		if total > MaxTotalBytes {
			return &ValidationError{Code: "skill_too_large", Path: absolute, Message: "Skill tree exceeds the total size limit"}
		}
		files = append(files, treeFile{path: filepath.ToSlash(relative), data: data, executable: mode.Perm()&0111 != 0})
		return nil
	})
	if err != nil {
		return Skill{}, nil, err
	}
	sort.Slice(files, func(i, j int) bool { return files[i].path < files[j].path })
	var skillData []byte
	for _, file := range files {
		if file.path == "SKILL.md" {
			skillData = file.data
			break
		}
	}
	if skillData == nil {
		return Skill{}, nil, &ValidationError{Code: "missing_skill_file", Path: absolute, Message: "Skill directory does not contain SKILL.md"}
	}
	header, err := parseFrontmatter(filepath.Join(absolute, "SKILL.md"), skillData)
	if err != nil {
		return Skill{}, nil, err
	}
	if filepath.Base(absolute) != header.Name {
		return Skill{}, nil, &ValidationError{Code: "name_mismatch", Path: absolute, Message: "Skill name must match its parent directory"}
	}
	hash := sha256.New()
	resources := []Resource{}
	for _, file := range files {
		sum := sha256.Sum256(file.data)
		sumText := hex.EncodeToString(sum[:])
		_, _ = hash.Write([]byte(file.path))
		_, _ = hash.Write([]byte{0})
		_, _ = hash.Write([]byte(sumText))
		_, _ = hash.Write([]byte{0})
		if file.path != "SKILL.md" {
			resources = append(resources, Resource{Path: file.path, Size: int64(len(file.data)), SHA256: sumText, Executable: file.executable})
		}
	}
	skill := Skill{
		Name: header.Name, Description: header.Description, Scope: scope, Root: absolute,
		Revision: hex.EncodeToString(hash.Sum(nil)), License: header.License, Compatibility: header.Compatibility,
		Metadata: header.Metadata, AllowedTools: header.AllowedTools, ResourceCount: len(resources), TotalBytes: total,
	}
	if content {
		skill.Content = string(skillData)
		skill.Resources = resources
	}
	return skill, files, nil
}

func Inspect(root string, scope Scope) (Skill, error) {
	skill, _, err := inspectTree(root, scope, true)
	return skill, err
}

func discoverScope(anchor string, scope Scope) ([]Skill, []Diagnostic, error) {
	base, err := scopeRoot(anchor)
	if err != nil {
		return []Skill{}, []Diagnostic{{Scope: scope, Path: anchor, Code: "invalid_registry", Message: err.Error()}}, nil
	}
	handle, err := os.Open(base)
	if errors.Is(err, os.ErrNotExist) {
		return []Skill{}, []Diagnostic{}, nil
	}
	if err != nil {
		return nil, nil, err
	}
	defer handle.Close()
	entries, err := handle.ReadDir(MaxSkills + 1)
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, nil, err
	}
	limited := len(entries) > MaxSkills
	if limited {
		entries = entries[:MaxSkills]
	}
	items := []Skill{}
	diagnostics := []Diagnostic{}
	if limited {
		diagnostics = append(diagnostics, Diagnostic{Scope: scope, Path: base, Code: "too_many_skills", Message: "Skill discovery limit reached"})
	}
	for _, entry := range entries {
		path := filepath.Join(base, entry.Name())
		if entry.Type()&os.ModeSymlink != 0 {
			diagnostics = append(diagnostics, Diagnostic{Scope: scope, Path: path, Code: "unsupported_symlink", Message: "Skill root cannot be a symlink"})
			continue
		}
		if !entry.IsDir() {
			continue
		}
		if _, err := os.Lstat(filepath.Join(path, "SKILL.md")); errors.Is(err, os.ErrNotExist) {
			continue
		}
		skill, _, err := inspectTree(path, scope, false)
		if err != nil {
			var validation *ValidationError
			if errors.As(err, &validation) {
				diagnostics = append(diagnostics, Diagnostic{Scope: scope, Path: validation.Path, Code: validation.Code, Message: validation.Message})
				continue
			}
			return nil, nil, err
		}
		items = append(items, skill)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Name < items[j].Name })
	sort.Slice(diagnostics, func(i, j int) bool {
		if diagnostics[i].Path == diagnostics[j].Path {
			return diagnostics[i].Code < diagnostics[j].Code
		}
		return diagnostics[i].Path < diagnostics[j].Path
	})
	return items, diagnostics, nil
}

func Discover(projectRoot, userHome string) (Catalog, error) {
	userItems, userDiagnostics, err := discoverScope(userHome, User)
	if err != nil {
		return Catalog{}, err
	}
	projectItems, projectDiagnostics, err := discoverScope(projectRoot, Project)
	if err != nil {
		return Catalog{}, err
	}
	selected := map[string]Skill{}
	for _, skill := range userItems {
		selected[skill.Name] = skill
	}
	shadowed := []Shadow{}
	for _, skill := range projectItems {
		if prior, exists := selected[skill.Name]; exists {
			shadowed = append(shadowed, Shadow{Name: skill.Name, SelectedScope: Project, SelectedRoot: skill.Root, ShadowedScope: prior.Scope, ShadowedRoot: prior.Root})
		}
		selected[skill.Name] = skill
	}
	items := make([]Skill, 0, len(selected))
	for _, skill := range selected {
		items = append(items, skill)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Name < items[j].Name })
	sort.Slice(shadowed, func(i, j int) bool { return shadowed[i].Name < shadowed[j].Name })
	diagnostics := append(userDiagnostics, projectDiagnostics...)
	sort.Slice(diagnostics, func(i, j int) bool {
		if diagnostics[i].Path == diagnostics[j].Path {
			return diagnostics[i].Code < diagnostics[j].Code
		}
		return diagnostics[i].Path < diagnostics[j].Path
	})
	return Catalog{Items: items, Diagnostics: diagnostics, Shadowed: shadowed}, nil
}

func ensureDirectory(path string) error {
	info, err := os.Lstat(path)
	if err == nil {
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return errors.New("skill registry path is not a regular directory")
		}
		return nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return os.Mkdir(path, 0755)
}

func registrationRoot(anchor string) (string, error) {
	if !filepath.IsAbs(anchor) {
		return "", errors.New("skill registration root must be absolute")
	}
	resolved, err := filepath.EvalSymlinks(anchor)
	if err != nil {
		return "", err
	}
	agents := filepath.Join(resolved, ".agents")
	if err := ensureDirectory(agents); err != nil {
		return "", err
	}
	root := filepath.Join(agents, "skills")
	if err := ensureDirectory(root); err != nil {
		return "", err
	}
	return root, nil
}

func Register(ctx context.Context, source, projectRoot, userHome string, scope Scope) (Skill, error) {
	if scope != Project && scope != User {
		return Skill{}, errors.New("skill scope must be project or user")
	}
	absolute, err := filepath.Abs(source)
	if err != nil {
		return Skill{}, err
	}
	sourceSkill, files, err := inspectTree(absolute, scope, true)
	if err != nil {
		return Skill{}, err
	}
	anchor := projectRoot
	if scope == User {
		anchor = userHome
	}
	root, err := registrationRoot(anchor)
	if err != nil {
		return Skill{}, err
	}
	target := filepath.Join(root, sourceSkill.Name)
	if err := os.Mkdir(target, 0755); err != nil {
		if errors.Is(err, os.ErrExist) {
			return Skill{}, fmt.Errorf("%w: %q in %s scope", ErrExists, sourceSkill.Name, scope)
		}
		return Skill{}, err
	}
	complete := false
	defer func() {
		if !complete {
			_ = os.RemoveAll(target)
		}
	}()
	write := func(file treeFile) error {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		path := filepath.Join(target, filepath.FromSlash(file.path))
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			return err
		}
		mode := os.FileMode(0644)
		if file.executable {
			mode = 0755
		}
		handle, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
		if err != nil {
			return err
		}
		_, writeErr := handle.Write(file.data)
		syncErr := handle.Sync()
		closeErr := handle.Close()
		if writeErr != nil {
			return writeErr
		}
		if syncErr != nil {
			return syncErr
		}
		return closeErr
	}
	for _, file := range files {
		if file.path == "SKILL.md" {
			continue
		}
		if err := write(file); err != nil {
			return Skill{}, err
		}
	}
	for _, file := range files {
		if file.path == "SKILL.md" {
			if err := write(file); err != nil {
				return Skill{}, err
			}
			break
		}
	}
	installed, err := Inspect(target, scope)
	if err != nil {
		return Skill{}, err
	}
	if installed.Revision != sourceSkill.Revision {
		return Skill{}, errors.New("registered Skill revision does not match source")
	}
	complete = true
	return installed, nil
}
