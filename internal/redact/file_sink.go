package redact

import (
	"context"
	"io"
	"sync"
)

// fileSink mediates only already filtered bytes. A slow inherited stdout file
// can block inside Write even after the child is killed. One in-flight block and
// one queued block plus a blocked producer block bound memory while allowing producer cancellation to return.
// No file flags are changed (TTY stdin can share the open file description).
// An uninterruptible destination callback can retain a filtered block until it
// returns or the CLI exits; it retains neither matcher nor secret input state.
type fileSink struct {
	ctx         context.Context
	destination io.Writer
	queue       chan []byte
	done        chan struct{}
	mu          sync.RWMutex
	closed      bool
	failed      func()
}

func newFileSink(ctx context.Context, destination io.Writer, failed func()) *fileSink {
	sink := &fileSink{ctx: ctx, destination: destination, queue: make(chan []byte, 1), done: make(chan struct{}), failed: failed}
	go sink.run()
	return sink
}
func (s *fileSink) run() {
	defer close(s.done)
	defer func() {
		for {
			select {
			case b := <-s.queue:
				if b == nil {
					return
				}
				clear(b)
			default:
				return
			}
		}
	}()
	for {
		select {
		case <-s.ctx.Done():
			return
		case b, ok := <-s.queue:
			if !ok {
				return
			}
			if s.ctx.Err() != nil {
				clear(b)
				return
			}
			n, err := s.destination.Write(b)
			if err == nil && n != len(b) {
				err = io.ErrShortWrite
			}
			clear(b)
			if err != nil {
				if s.ctx.Err() == nil {
					s.failed()
				}
				return
			}
		}
	}
}
func (s *fileSink) Write(p []byte) (int, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.closed {
		return 0, ErrClosed
	}
	consumed := 0
	for len(p) > 0 {
		if err := s.ctx.Err(); err != nil {
			return consumed, err
		}
		size := min(len(p), outputChunk)
		block := append([]byte{}, p[:size]...)
		select {
		case <-s.ctx.Done():
			clear(block)
			return consumed, s.ctx.Err()
		case <-s.done:
			clear(block)
			return consumed, ErrOutput
		case s.queue <- block:
			consumed += size
			p = p[size:]
		}
	}
	return consumed, nil
}
func (s *fileSink) close() error {
	s.mu.Lock()
	if !s.closed {
		s.closed = true
		close(s.queue)
	}
	s.mu.Unlock()
	select {
	case <-s.done:
		return nil
	case <-s.ctx.Done():
		return s.ctx.Err()
	}
}
