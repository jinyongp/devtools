package tasks

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
)

const coordinationVersion = 1

type receiptCoordination struct {
	Version       int    `json:"version"`
	RequestID     string `json:"request_id"`
	Fingerprint   string `json:"fingerprint"`
	ContextHash   string `json:"context_hash"`
	Result        Object `json:"result"`
	PayloadDigest string `json:"payload_digest"`
	FrameDigest   string `json:"frame_digest"`
}

type contextCoordination struct {
	Version       int    `json:"version"`
	ContextHash   string `json:"context_hash"`
	RunID         string `json:"run_id"`
	PayloadDigest string `json:"payload_digest"`
	FrameDigest   string `json:"frame_digest"`
}

type pendingRef struct {
	Kind          string `json:"kind"`
	Key           string `json:"key"`
	PayloadDigest string `json:"payload_digest"`
	PendingPath   string `json:"pending_path"`
	FinalPath     string `json:"final_path"`
}

type pendingManifest struct {
	Version     int          `json:"version"`
	RequestID   string       `json:"request_id"`
	FrameDigest string       `json:"frame_digest"`
	Receipt     pendingRef   `json:"receipt"`
	Contexts    []pendingRef `json:"contexts"`
}

func receiptImmutableDigest(requestID, fingerprint, contextHash string, result Object) string {
	return hash(Object{
		"request_id":   requestID,
		"fingerprint":  fingerprint,
		"context_hash": contextHash,
		"result":       result,
	})
}

func contextImmutableDigest(contextHash, runID string) string {
	return hash(Object{"context_hash": contextHash, "run_id": runID})
}

func newReceiptCoordination(requestID, fingerprint, contextHash string, result Object, frameDigest string) receiptCoordination {
	copy := copyObject(result)
	return receiptCoordination{
		Version:       coordinationVersion,
		RequestID:     requestID,
		Fingerprint:   fingerprint,
		ContextHash:   contextHash,
		Result:        copy,
		PayloadDigest: receiptImmutableDigest(requestID, fingerprint, contextHash, copy),
		FrameDigest:   frameDigest,
	}
}

func newContextCoordination(contextHash, runID, frameDigest string) contextCoordination {
	return contextCoordination{
		Version:       coordinationVersion,
		ContextHash:   contextHash,
		RunID:         runID,
		PayloadDigest: contextImmutableDigest(contextHash, runID),
		FrameDigest:   frameDigest,
	}
}

func (r receiptCoordination) valid(profileRequestID string) bool {
	return r.Version == coordinationVersion &&
		r.RequestID == profileRequestID &&
		validID(r.RequestID) &&
		r.Fingerprint != "" &&
		(r.ContextHash == "" || validDigest(r.ContextHash)) &&
		r.Result != nil &&
		validDigest(r.PayloadDigest) &&
		validDigest(r.FrameDigest) &&
		r.PayloadDigest == receiptImmutableDigest(r.RequestID, r.Fingerprint, r.ContextHash, r.Result)
}

func (c contextCoordination) valid(expectedHash string) bool {
	return c.Version == coordinationVersion &&
		c.ContextHash == expectedHash &&
		validDigest(c.ContextHash) &&
		validID(c.RunID) &&
		validDigest(c.PayloadDigest) &&
		validDigest(c.FrameDigest) &&
		c.PayloadDigest == contextImmutableDigest(c.ContextHash, c.RunID)
}

func coordinationShard(key string) (string, bool) {
	if len(key) < 2 {
		return "", false
	}
	shard := strings.ToLower(key[:2])
	for _, r := range shard {
		if r < '0' || r > '9' && r < 'a' || r > 'f' {
			return "", false
		}
	}
	return shard, true
}

func receiptPath(resolution v3Resolution, requestID string) (string, error) {
	if !validID(requestID) {
		return "", errors.New("invalid request ID")
	}
	shard, ok := coordinationShard(requestID)
	if !ok {
		return "", errors.New("invalid receipt shard")
	}
	return filepath.Join(resolution.Root, "receipts", shard, requestID+".json"), nil
}

func contextPath(resolution v3Resolution, contextHash string) (string, error) {
	if !validDigest(contextHash) {
		return "", errors.New("invalid context hash")
	}
	shard, ok := coordinationShard(contextHash)
	if !ok {
		return "", errors.New("invalid context shard")
	}
	return filepath.Join(resolution.Root, "contexts", shard, contextHash+".json"), nil
}

func ensureCoordinationParent(resolution v3Resolution, kind, key string) (string, error) {
	var shard string
	var ok bool
	switch kind {
	case "receipt":
		if !validID(key) {
			return "", errors.New("invalid receipt key")
		}
		shard, ok = coordinationShard(key)
	case "context":
		if !validDigest(key) {
			return "", errors.New("invalid context key")
		}
		shard, ok = coordinationShard(key)
	default:
		return "", errors.New("invalid coordination kind")
	}
	if !ok {
		return "", errors.New("invalid coordination shard")
	}
	parent, err := ensurePrivateDirectory(resolution.Root, kind+"s")
	if err != nil {
		return "", err
	}
	return ensurePrivateDirectory(parent, shard)
}

func writeCommittedReceipt(resolution v3Resolution, payload receiptCoordination) error {
	if !payload.valid(payload.RequestID) {
		return errors.New("invalid receipt payload")
	}
	parent, err := ensureCoordinationParent(resolution, "receipt", payload.RequestID)
	if err != nil {
		return err
	}
	return WritePrivate(filepath.Join(parent, payload.RequestID+".json"), payload)
}

func writeCommittedContext(resolution v3Resolution, payload contextCoordination) error {
	if !payload.valid(payload.ContextHash) {
		return errors.New("invalid context payload")
	}
	parent, err := ensureCoordinationParent(resolution, "context", payload.ContextHash)
	if err != nil {
		return err
	}
	return WritePrivate(filepath.Join(parent, payload.ContextHash+".json"), payload)
}

func readCommittedReceipt(resolution v3Resolution, requestID string) (receiptCoordination, bool, error) {
	var payload receiptCoordination
	path, err := receiptPath(resolution, requestID)
	if err != nil {
		return payload, false, err
	}
	if err := decodeOnePrivate(path, &payload); errors.Is(err, os.ErrNotExist) {
		return payload, false, nil
	} else if err != nil {
		return payload, false, err
	}
	if !payload.valid(requestID) {
		return payload, false, errors.New("invalid receipt payload")
	}
	return payload, true, nil
}

func readCommittedContext(resolution v3Resolution, contextHash string) (contextCoordination, bool, error) {
	var payload contextCoordination
	path, err := contextPath(resolution, contextHash)
	if err != nil {
		return payload, false, err
	}
	if err := decodeOnePrivate(path, &payload); errors.Is(err, os.ErrNotExist) {
		return payload, false, nil
	} else if err != nil {
		return payload, false, err
	}
	if !payload.valid(contextHash) {
		return payload, false, errors.New("invalid context payload")
	}
	return payload, true, nil
}

func readCoordinationPayload(path string, value any) error {
	if filepath.Base(path) == "" || strings.Contains(filepath.Base(path), string(os.PathSeparator)) {
		return errors.New("invalid coordination path")
	}
	return decodeOnePrivate(path, value)
}

func coordinationJSON(value any) ([]byte, error) {
	return json.Marshal(value)
}
