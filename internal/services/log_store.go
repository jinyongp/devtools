package services

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"sync"

	"github.com/jinyongp/devtools/internal/maintenance"
	"github.com/jinyongp/devtools/internal/platformfs"
	"github.com/jinyongp/devtools/internal/protocol"
)

const (
	logLogicalCap  = 1 << 20
	logPhysicalMax = 1280 << 10
)

type logIOStats struct {
	mu            sync.Mutex
	AppendBytes   int64
	RewriteBytes  int64
	PhysicalReads int64
	ReadBytes     int64
	Fsyncs        int64
	Compactions   int64
}

func (s *logIOStats) appendBytes(n int) {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.AppendBytes += int64(n)
	s.Fsyncs++
	s.mu.Unlock()
}

func (s *logIOStats) readBytes(n int) {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.PhysicalReads++
	s.ReadBytes += int64(n)
	s.mu.Unlock()
}

func (s *logIOStats) rewriteBytes(n int, compaction bool) {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.RewriteBytes += int64(n)
	s.Fsyncs++
	if compaction {
		s.Compactions++
	}
	s.mu.Unlock()
}

type boundedLogStore struct {
	path  string
	stats *logIOStats
}

func privateLogFile(file *os.File) (os.FileInfo, error) {
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if platformfs.CheckFile(file) != nil {
		return nil, errors.New("private log file required")
	}
	return info, nil
}

func privateLogDirectory(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !platformfs.DirectoryPrivate(path, info) {
		return errors.New("private process directory required")
	}
	return nil
}

func readPhysicalLog(path string, stats *logIOStats) ([]byte, bool, error) {
	file, err := platformfs.OpenPrivate(path, os.O_RDONLY, 0)
	if errors.Is(err, os.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	defer file.Close()
	info, err := privateLogFile(file.File)
	if err != nil {
		return nil, false, err
	}
	if info.Size() > logPhysicalMax {
		return nil, false, errors.New("bounded log exceeds physical maximum")
	}
	body, err := io.ReadAll(io.LimitReader(file, logPhysicalMax+1))
	if err != nil || len(body) > logPhysicalMax {
		if err == nil {
			err = errors.New("bounded log exceeds physical maximum")
		}
		return nil, false, err
	}
	stats.readBytes(len(body))
	return body, true, nil
}

func physicalLogSize(path string) (int64, bool, error) {
	file, err := platformfs.OpenPrivate(path, os.O_RDONLY, 0)
	if errors.Is(err, os.ErrNotExist) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, err
	}
	defer file.Close()
	info, err := privateLogFile(file.File)
	if err != nil {
		return 0, false, err
	}
	if info.Size() > logPhysicalMax {
		return 0, false, errors.New("bounded log exceeds physical maximum")
	}
	return info.Size(), true, nil
}

func logicalTail(body []byte) []byte {
	if len(body) <= logLogicalCap {
		return append([]byte{}, body...)
	}
	return append([]byte{}, body[len(body)-logLogicalCap:]...)
}

func syncAppend(path string, payload []byte) error {
	file, err := platformfs.OpenPrivate(path, os.O_WRONLY|os.O_APPEND, 0)
	if err != nil {
		return err
	}
	info, err := privateLogFile(file.File)
	if err != nil {
		_ = file.Close()
		return err
	}
	if info.Size() > logPhysicalMax || info.Size()+int64(len(payload)) > logPhysicalMax {
		_ = file.Close()
		return errors.New("bounded log exceeds physical maximum")
	}
	written := 0
	for written < len(payload) {
		n, writeErr := file.Write(payload[written:])
		if writeErr != nil {
			_ = file.Close()
			return writeErr
		}
		if n == 0 {
			_ = file.Close()
			return io.ErrShortWrite
		}
		written += n
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return err
	}
	return file.Close()
}

func (s boundedLogStore) write(p []byte) (int, error) {
	n := len(p)
	if n == 0 {
		return 0, nil
	}
	release, err := acquireLogLock(context.Background(), s.path, true)
	if err != nil {
		return 0, err
	}
	defer release()

	physicalSize, exists, err := physicalLogSize(s.path)
	if err != nil {
		return 0, err
	}
	if !exists {
		tail := logicalTail(p)
		if err := maintenance.Write(s.path, tail); err != nil {
			return 0, err
		}
		s.stats.rewriteBytes(len(tail), false)
		return n, nil
	}
	if len(p) >= logLogicalCap {
		tail := p[len(p)-logLogicalCap:]
		if err := maintenance.Write(s.path, tail); err != nil {
			return 0, err
		}
		s.stats.rewriteBytes(len(tail), false)
		return n, nil
	}
	if physicalSize+int64(len(p)) <= logPhysicalMax {
		if err := syncAppend(s.path, p); err != nil {
			return 0, err
		}
		s.stats.appendBytes(len(p))
		return n, nil
	}
	current, exists, err := readPhysicalLog(s.path, s.stats)
	if err != nil || !exists {
		if err == nil {
			err = errors.New("bounded log disappeared during compaction")
		}
		return 0, err
	}
	needed := logLogicalCap - len(p)
	if needed < 0 {
		needed = 0
	}
	if len(current) > needed {
		current = current[len(current)-needed:]
	}
	tail := make([]byte, 0, len(current)+len(p))
	tail = append(tail, current...)
	tail = append(tail, p...)
	if len(tail) > logLogicalCap {
		tail = tail[len(tail)-logLogicalCap:]
	}
	if err := maintenance.Write(s.path, tail); err != nil {
		return 0, err
	}
	s.stats.rewriteBytes(len(tail), true)
	return n, nil
}

func (s boundedLogStore) snapshot(ctx context.Context) ([]byte, bool, error) {
	release, err := acquireLogLock(ctx, s.path, false)
	if err != nil {
		return nil, false, err
	}
	defer release()
	body, exists, err := readPhysicalLog(s.path, s.stats)
	if err != nil || !exists {
		return nil, exists, err
	}
	return logicalTail(body), true, nil
}

func (s boundedLogStore) retire(ctx context.Context, beforeRemove func([]byte) *protocol.Error) (bool, *protocol.Error) {
	release, err := acquireLogLock(ctx, s.path, true)
	if err != nil {
		if ctx.Err() != nil {
			return false, protocol.ContextError(ctx.Err())
		}
		return false, storageErrorAt("acquire_log_lock", err)
	}
	defer release()
	body, exists, err := readPhysicalLog(s.path, s.stats)
	if err != nil {
		return false, storageErrorAt("read_log", err)
	}
	if !exists {
		return false, nil
	}
	logical := logicalTail(body)
	if beforeRemove != nil {
		if apiErr := beforeRemove(logical); apiErr != nil {
			return true, apiErr
		}
	}
	if err := removePrivateImage(ctx, s.path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return true, storageErrorAt("remove_log", err)
	}

	return true, nil
}

func (s boundedLogStore) restore(ctx context.Context, payload []byte) *protocol.Error {
	if len(payload) > logLogicalCap {
		return failure("invalid_argument")
	}
	release, err := acquireLogLock(ctx, s.path, true)
	if err != nil {
		if ctx.Err() != nil {
			return protocol.ContextError(ctx.Err())
		}
		return storageErrorAt("acquire_log_lock", err)
	}
	defer release()
	body, exists, err := readPhysicalLog(s.path, s.stats)
	if err != nil {
		return storageErrorAt("read_log", err)
	}
	if exists {
		if !bytes.Equal(logicalTail(body), payload) {
			return failure("revision_conflict")
		}
		return nil
	}
	if err := maintenance.Write(s.path, payload); err != nil {
		return storageErrorAt("restore_log", err)
	}
	return nil
}

func (s Store) logStore(id string) boundedLogStore {
	return boundedLogStore{path: s.path(id, "output.log")}
}

func (s Store) LogSnapshot(ctx context.Context, id string) ([]byte, bool, *protocol.Error) {
	if !validID(id) {
		return nil, false, failure("invalid_argument")
	}
	body, exists, err := s.logStore(id).snapshot(ctx)
	if err != nil {
		if ctx.Err() != nil {
			return nil, false, protocol.ContextError(ctx.Err())
		}
		return nil, false, storageErrorAt("read_log_snapshot", err)
	}
	return body, exists, nil
}

func (s Store) RetireLog(ctx context.Context, id string, beforeRemove func([]byte) *protocol.Error) (bool, *protocol.Error) {
	if !validID(id) {
		return false, failure("invalid_argument")
	}
	return s.logStore(id).retire(ctx, beforeRemove)
}

func (s Store) RestoreLog(ctx context.Context, id string, payload []byte) *protocol.Error {
	if !validID(id) {
		return failure("invalid_argument")
	}
	return s.logStore(id).restore(ctx, payload)
}
