// Package configedit evaluates typed, revision-guarded edits before publication.
package configedit

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"time"

	"github.com/jinyongp/devtools/internal/configcontract"
	"github.com/jinyongp/devtools/internal/maintenance"
	"github.com/jinyongp/devtools/internal/project"
	"github.com/jinyongp/devtools/internal/projectfile"
	"github.com/jinyongp/devtools/internal/protocol"
)

type Operation struct {
	Op      string         `json:"op"`
	Name    string         `json:"name,omitempty"`
	NewName string         `json:"new_name,omitempty"`
	Value   map[string]any `json:"value,omitempty"`
}
type Change struct {
	Op     string   `json:"op"`
	Path   []string `json:"path"`
	ToPath []string `json:"to_path,omitempty"`
}
type Impact struct {
	Commands         []string `json:"commands"`
	CommandCount     int      `json:"command_count"`
	ExternalUsage    string   `json:"external_usage"`
	RunningProcesses string   `json:"running_processes"`
}
type Result struct {
	ConfigPath       string   `json:"config_path"`
	PreviousRevision string   `json:"previous_revision"`
	Revision         string   `json:"revision"`
	Changed          bool     `json:"changed"`
	WouldChange      bool     `json:"would_change"`
	CandidateDigest  string   `json:"candidate_digest"`
	Changes          []Change `json:"changes"`
	Impact           Impact   `json:"impact"`
	Format           string   `json:"format"`
	CandidateFormat  string   `json:"candidate_format"`
}
type MigrationResult struct {
	Result
	Equivalent  bool   `json:"equivalent"`
	ApplyDigest string `json:"apply_digest"`
}
type Input struct {
	Revision    string
	DryRun      bool
	Format      bool
	Migrate     bool
	ApplyDigest string
	Operations  []Operation
	Runtime     string
}

func failure(code, reason string, path []string) *protocol.Error {
	e := protocol.NewError(code, "The configuration edit could not be applied. Inspect the indicated resource and refresh the configuration revision.", 3, map[string]any{"reason": reason, "sources": []configcontract.Source{{Kind: "config", Path: append([]string{}, path...)}}})
	if code == "invalid_argument" {
		e.ExitCode = 2
	}
	return e
}
func clone(m map[string]any) map[string]any {
	// TOML and accepted JSON use int64. Preserve numeric types rather than making
	// a float64 round trip that could alter port values or explicit declarations.
	out := map[string]any{}
	for k, v := range m {
		if child, ok := v.(map[string]any); ok {
			out[k] = clone(child)
		} else if a, ok := v.([]any); ok {
			out[k] = append([]any{}, a...)
		} else {
			out[k] = v
		}
	}
	return out
}
func keys(m map[string]any) []string {
	out := []string{}
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
func object(m map[string]any, key string) map[string]any {
	v, _ := m[key].(map[string]any)
	if v == nil {
		v = map[string]any{}
		m[key] = v
	}
	return v
}

func validateOperations(operations []Operation) *protocol.Error {
	if len(operations) == 0 {
		return failure("invalid_argument", "missing_operations", nil)
	}
	seen := map[string]bool{}
	for i, op := range operations {
		input := map[string]any{"op": op.Op}
		if op.Name != "" {
			input["name"] = op.Name
		}
		if op.NewName != "" {
			input["new_name"] = op.NewName
		}
		if op.Value != nil {
			input["value"] = op.Value
		}
		b, _ := json.Marshal(input)
		v, issue := configcontract.Decode(b)
		if issue != nil || len(configcontract.Validate("Operation", v)) > 0 {
			return failure("invalid_argument", "invalid_operation", []string{"operations", stringIndex(i)})
		}
		kind, action, _ := strings.Cut(op.Op, ".")
		identity := kind + "\x00" + op.Name
		if seen[identity] {
			return failure("invalid_argument", "conflicting_operations", []string{"operations", stringIndex(i)})
		}
		seen[identity] = true
		if action == "rename" && op.Name != op.NewName {
			key := kind + "\x00" + op.NewName
			if seen[key] {
				return failure("invalid_argument", "conflicting_operations", nil)
			}
			seen[key] = true
		}
	}
	return nil
}
func stringIndex(i int) string { b, _ := json.Marshal(i); return string(b) }

func Evaluate(ctx context.Context, path string, in Input) (out any, editErr *protocol.Error) {
	var observed *project.Document
	defer func() {
		if editErr == nil {
			return
		}
		if editErr.Details == nil {
			editErr.Details = map[string]any{}
		}
		target, _ := editErr.Details["target"].(map[string]any)
		if target == nil {
			target = map[string]any{"kind": "config", "name": project.Filename}
		}
		target["config_path"] = path
		if sources, ok := editErr.Details["sources"].([]configcontract.Source); ok {
			for i, source := range sources {
				if observed != nil && source.Kind == "config" && source.Offset == nil {
					sources[i] = observed.Source(source.Path)
				}
				if len(source.Path) >= 2 {
					kind := map[string]string{"tools": "tool", "presets": "preset", "commands": "command", "ports": "port", "proxies": "proxy"}[source.Path[0]]
					if kind != "" {
						target["kind"] = kind
						target["name"] = source.Path[1]
					}
				}
			}
			editErr.Details["sources"] = sources
		}
		editErr.Details["target"] = target
	}()
	if in.Format && in.Migrate {
		return nil, failure("invalid_argument", "conflicting_inputs", nil)
	}
	if in.Migrate && !in.DryRun && (in.ApplyDigest == "" || in.Revision == "") {
		return nil, failure("invalid_argument", "missing_migration_approval", nil)
	}
	for _, op := range in.Operations {
		if !strings.HasSuffix(op.Op, ".add") && in.Revision == "" {
			return nil, failure("invalid_argument", "missing_revision", nil)
		}
	}
	if !in.Format && !in.Migrate {
		if e := validateOperations(in.Operations); e != nil {
			return nil, e
		}
	}
	if in.Revision != "" && len(configcontract.Validate("Revision", in.Revision)) > 0 {
		return nil, failure("invalid_argument", "invalid_revision", nil)
	}
	if !in.DryRun && in.Runtime != "" {
		lockCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		defer cancel()
		key := lockKey(path)
		sum := sha256.Sum256([]byte(key))
		release, err := maintenance.LockExclusive(lockCtx, filepath.Join(in.Runtime, "config-edit", hex.EncodeToString(sum[:])+".lock"))
		if err != nil {
			if ctx.Err() != nil {
				return nil, protocol.ContextError(ctx.Err())
			}
			if lockCtx.Err() != nil {
				return nil, failure("config_busy", "writer_busy", nil)
			}
			return nil, projectfile.Error("io_error", "lock_failed", err)
		}
		defer release()
	}
	snapshot, e := projectfile.Read(path)
	if e != nil {
		return nil, e
	}
	if in.Revision != "" && snapshot.Revision != in.Revision {
		return nil, failure("config_revision_conflict", "revision_mismatch", nil)
	}
	doc, e := project.ParseDocument(snapshot.Bytes, path, filepath.Dir(path))
	if e != nil {
		return nil, e
	}
	observed = doc
	if e := projectfile.CheckWritable(snapshot); e != nil {
		return nil, e
	}
	if doc.Format == "legacy" && !in.Format && !in.Migrate {
		return nil, failure("config_migration_required", "legacy_format", nil)
	}
	desired := clone(doc.Raw)
	changes := []Change{}
	var candidate []byte
	if in.Migrate {
		desired, e = migration(doc)
		if e != nil {
			return nil, e
		}
		if !reflect.DeepEqual(desired, doc.Raw) {
			changes = append(changes, Change{Op: "migrate", Path: []string{}})
		}
	} else if !in.Format {
		for _, op := range in.Operations {
			kind, action, _ := strings.Cut(op.Op, ".")
			if kind == "project" {
				before := clone(desired)
				merge(desired, op.Value, nil)
				if !reflect.DeepEqual(before, desired) {
					changes = append(changes, Change{Op: op.Op, Path: []string{}})
				}
				continue
			}
			group := kind + "s"
			if kind == "proxy" {
				group = "proxies"
			}
			resources := object(desired, group)
			before, exists := resources[op.Name]
			path := []string{group, op.Name}
			switch action {
			case "add":
				if exists {
					return nil, failure("config_resource_exists", "already_exists", path)
				}
				resources[op.Name] = clone(op.Value)
			case "update":
				if !exists {
					return nil, failure("config_resource_not_found", "not_found", path)
				}
				current, ok := before.(map[string]any)
				if !ok {
					return nil, failure("invalid_config", "invalid_type", path)
				}
				next := clone(current)
				merge(next, op.Value, path)
				resources[op.Name] = next
			case "remove":
				if !exists {
					return nil, failure("config_resource_not_found", "not_found", path)
				}
				delete(resources, op.Name)
			case "rename":
				if !exists {
					return nil, failure("config_resource_not_found", "not_found", path)
				}
				if op.NewName == op.Name {
					continue
				}
				if _, exists := resources[op.NewName]; exists {
					return nil, failure("config_resource_exists", "already_exists", []string{group, op.NewName})
				}
				delete(resources, op.Name)
				resources[op.NewName] = before
			}
			after, afterExists := resources[op.Name]
			if exists == afterExists && reflect.DeepEqual(before, after) {
				continue
			}
			change := Change{Op: op.Op, Path: path}
			if action == "rename" {
				change.ToPath = []string{group, op.NewName}
			}
			changes = append(changes, change)
		}
	}
	if in.Format {
		candidate, e = format(doc)
		if e != nil {
			return nil, e
		}
		if !bytes.Equal(candidate, doc.Bytes) {
			changes = append(changes, Change{Op: "format", Path: []string{}})
		}
	} else {
		candidate, e = write(doc, desired)
		if e != nil {
			return nil, e
		}
		if in.Migrate && doc.Format == "legacy" {
			candidate, e = migrationComments(doc, candidate)
			if e != nil {
				return nil, e
			}
		}
	}
	if len(candidate) > configcontract.Limit {
		return nil, failure("invalid_config", "limit_exceeded", nil)
	}
	next, e := project.ParseDocument(candidate, path, filepath.Dir(path))
	if e != nil {
		causes, _ := e.Details["causes"].([]any)
		for _, op := range in.Operations {
			if strings.HasSuffix(op.Op, ".remove") {
				kind, _, _ := strings.Cut(op.Op, ".")
				for _, value := range causes {
					cause, _ := value.(map[string]any)
					target, _ := cause["target"].(map[string]any)
					if cause["reason"] == "unknown_reference" && target["kind"] == kind && target["name"] == op.Name {
						e.Code = "config_reference_conflict"
					}
				}
			}
		}
		return nil, e
	}
	if in.Migrate && !project.Equivalent(doc.Context, next.Context) {
		return nil, failure("config_migration_conflict", "execution_changed", nil)
	}
	if in.Format && !project.Equivalent(doc.Context, next.Context) {
		return nil, failure("config_preservation_failed", "execution_changed", nil)
	}
	if !reflect.DeepEqual(next.Raw, desired) && !in.Format {
		return nil, failure("config_preservation_failed", "document_changed", nil)
	}
	digest := sha256.Sum256(candidate)
	result := Result{ConfigPath: path, PreviousRevision: snapshot.Revision, Revision: snapshot.Revision, WouldChange: !bytes.Equal(candidate, doc.Bytes), CandidateDigest: hex.EncodeToString(digest[:]), Changes: changes, Impact: impact(doc.Context, next.Context), Format: doc.Format, CandidateFormat: next.Format}
	applyDigest := sha256.Sum256(append([]byte("config-migrate-v1\x00"+snapshot.Revision+"\x0010\x00registry-v1\x00"), candidate...))
	if in.Migrate && in.ApplyDigest != "" && in.ApplyDigest != hex.EncodeToString(applyDigest[:]) {
		return nil, failure("config_migration_conflict", "digest_mismatch", nil)
	}
	if result.WouldChange && !in.DryRun {
		now, e := projectfile.Publish(ctx, snapshot, candidate)
		if e != nil {
			return nil, e
		}
		result.Changed = true
		result.Revision = now.Revision
	}
	if in.Migrate {
		return MigrationResult{Result: result, Equivalent: true, ApplyDigest: hex.EncodeToString(applyDigest[:])}, nil
	}
	return result, nil
}

func merge(dst, patch map[string]any, path []string) {
	for k, v := range patch {
		if v == nil {
			delete(dst, k)
			continue
		}
		obj, ok := v.(map[string]any)
		whole := len(path) > 0 && path[len(path)-1] == "tool_overrides"
		if ok && !whole {
			next, _ := dst[k].(map[string]any)
			if next == nil {
				if len(obj) == 0 {
					continue
				}
				next = map[string]any{}
			} else {
				next = clone(next)
			}
			merge(next, obj, append(append([]string{}, path...), k))
			dst[k] = next
		} else {
			dst[k] = v
		}
	}
}
func impact(a, b project.Context) Impact {
	before := project.Effective(a)
	after := project.Effective(b)
	ac := before["commands"].(map[string]any)
	bc := after["commands"].(map[string]any)
	delete(before, "commands")
	delete(after, "commands")
	contextChanged := !reflect.DeepEqual(before, after)
	names := map[string]bool{}
	for n := range ac {
		names[n] = true
	}
	for n := range bc {
		names[n] = true
	}
	out := Impact{Commands: []string{}, ExternalUsage: "not_checked", RunningProcesses: "not_checked"}
	for n := range names {
		if contextChanged || !reflect.DeepEqual(ac[n], bc[n]) {
			out.Commands = append(out.Commands, n)
		}
	}
	sort.Strings(out.Commands)
	out.CommandCount = len(out.Commands)
	return out
}
