package services

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"

	"github.com/jinyongp/devtools/internal/idref"
	"github.com/jinyongp/devtools/internal/protocol"
	"github.com/jinyongp/devtools/internal/tasks"
)

// ResolveExecution includes reserved and archived execution IDs. An ended or
// archived ID still prevents the same prefix selecting a different execution.
func (s Store) ResolveExecution(ctx context.Context, input string) (string, *protocol.Error) {
	if idref.UUID.Exact(input) {
		return input, nil
	}
	names, err := s.executionNames()
	if err != nil {
		return "", storageErrorAt("enumerate_execution_ids", err)
	}
	id, condition := idref.Resolve(ctx, idref.UUID, input, slices.Values(names))
	return id, idref.DescribeAmbiguity(condition, func(candidate string) string {
		if ctx.Err() != nil {
			return ""
		}
		record, err := readDiskRecord(s.path(candidate, "record.json"))
		if err != nil || !validRecord(candidate, record.Metadata) {
			return ""
		}
		return record.Metadata.Command
	})
}

func (s Store) executionNames() ([]string, error) {
	return idref.DirectoryNames(s.root())
}

type executionBinding struct {
	ID        string `json:"id"`
	Prefix    string `json:"prefix"`
	Signature string `json:"signature"`
}

func executionSelectionSignature(q Request) string {
	input, _ := json.Marshal(struct {
		Action, ID  string
		Bundle, Env *string
		Capture     *bool
	}{q.Action, q.ID, q.Bundle, q.Env, q.Capture})
	sum := sha256.Sum256(input)
	return hex.EncodeToString(sum[:])
}

func (s Store) executionBindingPath(requestID string) string {
	return filepath.Join(s.root(), "request-targets", requestID+".json")
}

func (s Store) readExecutionBinding(requestID string) (executionBinding, bool, *protocol.Error) {
	var binding executionBinding
	err := tasks.ReadPrivate(s.executionBindingPath(requestID), &binding)
	if errors.Is(err, os.ErrNotExist) {
		return binding, false, nil
	}
	if err != nil || !validID(binding.ID) || !idref.UUID.Valid(binding.Prefix) ||
		idref.UUID.Exact(binding.Prefix) || !idref.Matches(binding.ID, binding.Prefix) || len(binding.Signature) != sha256.Size*2 {
		return binding, false, storageErrorAt("read_execution_binding", err)
	}
	return binding, true, nil
}

func (s Store) lockExecutionRequest(ctx context.Context, id string) (func(), *protocol.Error) {
	unlock, err := tasks.Lock(ctx, filepath.Join(s.root(), "operation-locks", "request-"+id+".lock"))
	if err != nil {
		return nil, storageErrorAt("lock_process_request", err)
	}
	return unlock, nil
}

// BindExecutionRequest fixes the user's target before restart can prepare
// network resources. The existing receipt keeps its on-disk format and still
// proves the complete canonical operation, including its prepared network input.
func (s Store) BindExecutionRequest(ctx context.Context, q *Request) *protocol.Error {
	if !validID(q.RequestID) || q.Action != "stop" && q.Action != "restart" {
		return failure("invalid_argument")
	}
	unlock, condition := s.lockExecutionRequest(ctx, q.RequestID)
	if condition != nil {
		return condition
	}
	defer unlock()
	binding, exists, condition := s.readExecutionBinding(q.RequestID)
	if condition != nil {
		return condition
	}
	if exists {
		if q.ID != binding.ID && q.ID != binding.Prefix {
			return failure("request_conflict")
		}
		q.ID = binding.ID
		if executionSelectionSignature(*q) != binding.Signature {
			return failure("request_conflict")
		}
		return nil
	}
	if idref.UUID.Exact(q.ID) {
		return nil
	}
	if prior, condition := s.HasRequest(q.RequestID); condition != nil {
		return condition
	} else if prior {
		return RequestRecoveryError("process", q.RequestID)
	}
	input := q.ID
	id, condition := s.ResolveExecution(ctx, input)
	if condition != nil {
		return condition
	}
	// Validate a matching reserved name before creating the binding.
	if _, condition = s.readContext(ctx, id); condition != nil {
		return condition
	}
	q.ID = id
	path := s.executionBindingPath(q.RequestID)
	if err := tasks.PrivateDir(filepath.Dir(path)); err != nil {
		return storageErrorAt("create_execution_binding", err)
	}
	if err := tasks.WritePrivate(path, executionBinding{ID: id, Prefix: input, Signature: executionSelectionSignature(*q)}); err != nil {
		return storageErrorAt("write_execution_binding", err)
	}
	return nil
}
