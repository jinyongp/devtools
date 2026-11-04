package ports

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"

	"github.com/jinyongp/devtools/internal/project"
	"github.com/jinyongp/devtools/internal/protocol"
)

// RunOwner identifies the controller holding a service claim, never the app PID.
// It is ephemeral lock metadata, excluded from registry and backup documents.
type RunOwner struct {
	Kind        string `json:"kind"`
	Command     string `json:"command"`
	ExecutionID string `json:"execution_id,omitempty"`
}

type runMetadata struct {
	Version    int    `json:"version"`
	Generation string `json:"generation"`
	PID        int    `json:"pid"`
	Identity   string `json:"identity"`
	RunOwner
}

type metadataFile interface {
	io.ReadWriteSeeker
	Truncate(int64) error
}

// Byte zero is reserved for the Windows lock. Metadata starts after that byte
// so another controller can read it without bypassing the exclusive lease.
func publishRunOwner(lease io.Closer, owner RunOwner) {
	f, ok := lease.(metadataFile)
	if !ok {
		return
	}
	if f.Truncate(1) != nil {
		return
	}
	identity, err := runProcessIdentity(os.Getpid())
	if err != nil {
		return
	}
	generation := make([]byte, 16)
	if _, err = rand.Read(generation); err != nil {
		return
	}
	if owner.Kind == "" {
		owner.Kind = "foreground"
	}
	metadata := runMetadata{Version: 1, Generation: hex.EncodeToString(generation), PID: os.Getpid(), Identity: identity, RunOwner: owner}
	data, err := json.Marshal(metadata)
	if err != nil {
		return
	}
	if _, err = f.Seek(1, io.SeekStart); err == nil {
		_, _ = f.Write(data)
	}
}

func unavailableOwner(reason string) map[string]any {
	return map[string]any{"status": "unavailable", "kind": nil, "pid": nil, "execution_id": nil, "command": nil, "reason": reason}
}

func failedOwnerObservation(reason string, cause error) map[string]any {
	if errors.Is(cause, os.ErrPermission) {
		reason = "permission_denied"
	}
	return unavailableOwner(reason)
}

// The caller has observed this exact lease as busy. A bounded double read and
// the OS process creation identity exclude partial publication and PID reuse.
func activeRunError(lease io.Closer) *protocol.Error {
	owner := unavailableOwner("metadata_unavailable")
	f, ok := lease.(metadataFile)
	if ok {
		read := func() ([]byte, error) {
			if _, err := f.Seek(1, io.SeekStart); err != nil {
				return nil, err
			}
			return io.ReadAll(io.LimitReader(f, 4097))
		}
		data, err := read()
		if err != nil {
			owner = failedOwnerObservation("metadata_unavailable", err)
		}
		if err == nil && len(data) == 0 {
			owner = unavailableOwner("legacy_run")
		}
		var metadata runMetadata
		if err == nil && len(data) > 0 && len(data) <= 4096 && json.Unmarshal(data, &metadata) == nil && metadata.Version == 1 && len(metadata.Generation) == 32 && metadata.PID > 0 {
			identity, identityErr := runProcessIdentity(metadata.PID)
			again, readErr := read()
			if identityErr != nil {
				owner = failedOwnerObservation("process_unavailable", identityErr)
			} else if readErr != nil {
				owner = failedOwnerObservation("metadata_unavailable", readErr)
			} else if metadata.ExecutionID != "" && !protocol.ValidUUID(metadata.ExecutionID) || metadata.Command != "" && !project.ValidCommand(metadata.Command) {
				owner = unavailableOwner("metadata_unavailable")
			} else if identity == metadata.Identity && bytes.Equal(data, again) && (metadata.Kind == "foreground" || metadata.Kind == "managed" || metadata.Kind == "global") {
				var executionID, command any
				if metadata.ExecutionID != "" {
					executionID = metadata.ExecutionID
				}
				if metadata.Command != "" {
					command = metadata.Command
				}
				owner = map[string]any{"status": "verified", "kind": metadata.Kind, "pid": metadata.PID, "execution_id": executionID, "command": command, "reason": nil}
			} else {
				owner = unavailableOwner("state_changed")
			}
		}
	}
	e := fail("port_run_active")
	e.Details = map[string]any{"facts": map[string]any{"owner": owner}}
	return e
}

func clearRunOwner(lease io.Closer) {
	if f, ok := lease.(metadataFile); ok {
		_ = f.Truncate(1)
	}
}
