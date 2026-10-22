// Package projectfile publishes existing project files without imposing the
// private-store permission policy on a shared checkout.
package projectfile

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"syscall"

	"github.com/jinyongp/devtools/internal/configcontract"
	"github.com/jinyongp/devtools/internal/location"
	"github.com/jinyongp/devtools/internal/protocol"
)

type Snapshot struct {
	Path               string
	Bytes              []byte
	Revision           string
	Info               os.FileInfo
	parent             os.FileInfo
	identity, security string
}

func Error(code, reason string, cause error) *protocol.Error {
	exit := 3
	if code == "io_error" || code == "config_commit_uncertain" {
		exit = 1
	}
	e := protocol.NewError(code, map[string]string{"io_error": "Cannot read or publish the project file.", "config_unsafe_target": "Use a writable regular project file with one hard link.", "config_revision_conflict": "The project file changed. Inspect it and review the intended edit again.", "config_write_unsupported": "This file cannot be published while preserving its permissions.", "config_commit_uncertain": "The publication outcome needs inspection before another edit."}[code], exit, map[string]any{"reason": reason, "effects": "none"})
	if code == "config_commit_uncertain" {
		e.Details["phase"] = "observe"
		if reason == "publication_uncertain" {
			e.Details["phase"] = "publish"
		}
	}
	var errno syscall.Errno
	if errors.As(cause, &errno) {
		e.Details["facts"] = map[string]any{"os_code": uint64(errno)}
	}
	var call *os.SyscallError
	if errors.As(cause, &call) {
		e = atStage(e, call.Syscall)
	}
	return e
}

// Stages are code-defined labels. Never copy raw OS error text, paths, file
// contents, or attribute/ACL values into diagnostic facts.
func atStage(e *protocol.Error, stage string) *protocol.Error {
	facts, _ := e.Details["facts"].(map[string]any)
	if facts == nil {
		facts = map[string]any{}
		e.Details["facts"] = facts
	}
	if inner, ok := facts["stage"].(string); ok {
		stage += "." + inner
	}
	facts["stage"] = stage
	return e
}

func ioFailure(stage string, cause error) *protocol.Error {
	return atStage(Error("io_error", ioReason(cause), cause), stage)
}

func Read(path string) (*Snapshot, *protocol.Error) {
	// Opening a FIFO must not block before the regular-file check below.
	f, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, ioFailure("read.open", err)
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, ioFailure("read.stat", err)
	}
	if !info.Mode().IsRegular() {
		return nil, Error("config_unsafe_target", "not_regular", nil)
	}
	data, err := io.ReadAll(io.LimitReader(f, configcontract.Limit+1))
	if err != nil {
		return nil, ioFailure("read.content", err)
	}
	if len(data) > configcontract.Limit {
		return nil, protocol.NewError("invalid_config", "Project configuration exceeds 2 MiB.", 3, map[string]any{"reason": "limit_exceeded"})
	}
	identity, security, err := metadata(f, info)
	if err != nil {
		return nil, ioFailure("read.metadata", err)
	}
	canonical, err := location.Canonical(path)
	if err != nil {
		return nil, ioFailure("read.canonicalize", err)
	}
	parent, err := os.Stat(filepath.Dir(path))
	if err != nil {
		return nil, ioFailure("read.parent_stat", err)
	}
	h := sha256.New()
	for _, field := range [][]byte{[]byte("devtools-config-revision-v1"), []byte(canonical), []byte(identity), []byte(security), data} {
		_ = binary.Write(h, binary.BigEndian, uint64(len(field)))
		_, _ = h.Write(field)
	}
	return &Snapshot{Path: path, Bytes: data, Revision: hex.EncodeToString(h.Sum(nil)), Info: info, identity: identity, security: security, parent: parent}, nil
}
func ioReason(err error) string {
	if errors.Is(err, os.ErrPermission) {
		return "permission_denied"
	}
	if errors.Is(err, os.ErrNotExist) {
		return "not_found"
	}
	return "io_failure"
}
func (s *Snapshot) Recheck() *protocol.Error {
	now, e := Read(s.Path)
	if e != nil {
		return e
	}
	if now.Revision != s.Revision || now.security != s.security || !os.SameFile(now.Info, s.Info) || !os.SameFile(now.parent, s.parent) {
		return Error("config_revision_conflict", "target_changed", nil)
	}
	return nil
}

func CheckWritable(s *Snapshot) *protocol.Error {
	if err := safeTarget(s); err != nil {
		return Error("config_unsafe_target", "unsafe_file", err)
	}
	return nil
}
func Publish(ctx context.Context, s *Snapshot, data []byte) (*Snapshot, *protocol.Error) {
	return publish(ctx, s, data, publication{})
}

// Operation-local hooks exercise crash and publication boundaries without global
// state or production environment-variable failpoints.
type publication struct {
	stage   func(string)
	replace func(context.Context, *Snapshot, *Snapshot) (bool, bool, string, error)
}

func publish(ctx context.Context, s *Snapshot, data []byte, ops publication) (*Snapshot, *protocol.Error) {
	stage := func(name string) {
		if ops.stage != nil {
			ops.stage(name)
		}
	}
	if e := protocol.ContextError(ctx.Err()); e != nil {
		return nil, e
	}
	if err := safeTarget(s); err != nil {
		return nil, Error("config_unsafe_target", "unsafe_file", err)
	}
	if e := s.Recheck(); e != nil {
		return nil, atStage(e, "publish.initial_recheck")
	}
	f, err := os.CreateTemp(filepath.Dir(s.Path), ".devtools-edit-*")
	if err != nil {
		return nil, ioFailure("publish.create_candidate", err)
	}
	name := f.Name()
	cleanup := true
	defer func() {
		_ = f.Close()
		if cleanup {
			_ = os.Remove(name)
		}
	}()
	stage("created")
	if err = copyMetadata(s, f); err != nil {
		return nil, atStage(Error("config_write_unsupported", "permission_preservation", err), "publish.copy_metadata")
	}
	writeStage := "publish.write_candidate"
	if _, err = f.Write(data); err == nil {
		stage("written")
		writeStage = "publish.sync_candidate"
		err = f.Sync()
		if err == nil {
			stage("flushed")
		}
	}
	if err == nil {
		writeStage = "publish.close_candidate"
		err = f.Close()
	}
	if err != nil {
		return nil, ioFailure(writeStage, err)
	}
	candidate, e := Read(name)
	if e != nil {
		return nil, atStage(e, "publish.read_candidate")
	}
	if !bytes.Equal(candidate.Bytes, data) || candidate.security != s.security {
		return nil, Error("config_write_unsupported", "permission_preservation", nil)
	}
	if e := protocol.ContextError(ctx.Err()); e != nil {
		return nil, e
	}
	if err = safeTarget(s); err != nil {
		return nil, Error("config_unsafe_target", "unsafe_file", err)
	}
	if e := s.Recheck(); e != nil {
		return nil, atStage(e, "publish.final_recheck")
	}
	publishFile := ops.replace
	if publishFile == nil {
		publishFile = replace
	}
	stage("before_publish")
	committed, uncertain, backup, publishErr := publishFile(ctx, s, candidate)
	if committed {
		stage("published")
	}
	if publishErr != nil || uncertain {
		if committed || uncertain {
			cleanup = false
			e := atStage(Error("config_commit_uncertain", "publication_uncertain", publishErr), "publish.replace")
			effect := "unknown"
			if committed {
				effect = "committed"
			}
			e.Details["effects"] = effect
			e.Details["retry"] = "inspect_first"
			facts, _ := e.Details["facts"].(map[string]any)
			if facts == nil {
				facts = map[string]any{}
			}
			digest := sha256.Sum256(data)
			facts["candidate_digest"] = hex.EncodeToString(digest[:])
			if backup != "" {
				facts["backup_name"] = filepath.Base(backup)
			}
			e.Details["facts"] = facts
			return nil, e
		}
		if e := protocol.ContextError(publishErr); e != nil {
			e.Details = map[string]any{"effects": "none"}
			return nil, e
		}
		var specific *protocol.Error
		if errors.As(publishErr, &specific) {
			return nil, specific
		}
		return nil, ioFailure("publish.replace", publishErr)
	}
	now, e := Read(s.Path)
	if e != nil || !bytes.Equal(now.Bytes, data) || now.security != s.security || !os.SameFile(now.Info, candidate.Info) {
		cleanup = false
		field := "identity"
		switch {
		case e != nil:
			field = "read"
		case !bytes.Equal(now.Bytes, data):
			field = "content"
		case now.security != s.security:
			field = "permissions"
		}
		failure := atStage(Error("config_commit_uncertain", "post_publication_observation", nil), "publish.observe")
		failure.Details["effects"] = "committed"
		failure.Details["retry"] = "inspect_first"
		failure.Details["facts"].(map[string]any)["field"] = field
		if e != nil {
			failure.Details["causes"] = []any{map[string]any{"code": e.Code, "details": e.Details}}
		}
		if backup != "" {
			failure.Details["facts"].(map[string]any)["backup_name"] = filepath.Base(backup)
		}
		return nil, failure
	}
	if backup != "" {
		if err := os.Remove(backup); err != nil {
			failure := atStage(Error("config_commit_uncertain", "backup_cleanup", err), "publish.backup_cleanup")
			failure.Details["effects"] = "committed"
			failure.Details["retry"] = "inspect_first"
			failure.Details["facts"].(map[string]any)["backup_name"] = filepath.Base(backup)
			return nil, failure
		}
	}
	return now, nil
}
