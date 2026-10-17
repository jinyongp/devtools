package process

import (
	"errors"
	"golang.org/x/sys/windows"
	"io"
	"os"
	"sync"
	"time"
)

// Direct files retain console behavior. Only non-file streams need copy pipes.
// Caller-owned streams are never closed by the launcher.
type nativeStreams struct {
	files       [3]*os.File
	childOwned  []*os.File
	parentOwned []*os.File
	copies      []func() error
	copied      chan error
	inputPipe   *os.File
	outputMu    sync.Mutex
	finishOnce  sync.Once
	finishErr   error
}
type nativeOutput struct {
	target io.Writer
	mu     *sync.Mutex
}

func (w nativeOutput) Write(body []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.target.Write(body)
}
func prepareNativeStreams(in io.Reader, out, diagnostic io.Writer) (s *nativeStreams, err error) {
	s = &nativeStreams{}
	defer func(owned *nativeStreams) {
		if err != nil {
			owned.closeAll()
		}
	}(s)
	if f, ok := in.(*os.File); ok && f != nil {
		s.files[0] = f
	} else if in == nil {
		f, e := os.Open(os.DevNull)
		if e != nil {
			return nil, e
		}
		s.files[0] = f
		s.childOwned = append(s.childOwned, f)
	} else {
		reader, writer, e := os.Pipe()
		if e != nil {
			return nil, e
		}
		s.files[0] = reader
		s.childOwned = append(s.childOwned, reader)
		s.parentOwned = append(s.parentOwned, writer)
		s.inputPipe = writer
		s.copies = append(s.copies, func() error {
			defer writer.Close()
			_, e := io.Copy(writer, in)
			if errors.Is(e, os.ErrClosed) || errors.Is(e, windows.ERROR_BROKEN_PIPE) || errors.Is(e, windows.ERROR_NO_DATA) {
				return nil
			}
			return e
		})
	}
	for index, target := range []io.Writer{out, diagnostic} {
		if f, ok := target.(*os.File); ok && f != nil {
			s.files[index+1] = f
			continue
		}
		if target == nil {
			f, e := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
			if e != nil {
				return nil, e
			}
			s.files[index+1] = f
			s.childOwned = append(s.childOwned, f)
			continue
		}
		reader, writer, e := os.Pipe()
		if e != nil {
			return nil, e
		}
		s.files[index+1] = writer
		s.childOwned = append(s.childOwned, writer)
		s.parentOwned = append(s.parentOwned, reader)
		s.copies = append(s.copies, func() error {
			defer reader.Close()
			_, e := io.Copy(nativeOutput{target, &s.outputMu}, reader)
			return e
		})
	}
	return s, nil
}
func (s *nativeStreams) start() {
	s.copied = make(chan error, len(s.copies))
	for _, f := range s.childOwned {
		_ = f.Close()
	}
	s.childOwned = nil
	for _, copy := range s.copies {
		go func() { s.copied <- copy() }()
	}
}
func (s *nativeStreams) closeAll() {
	for _, f := range s.childOwned {
		_ = f.Close()
	}
	for _, f := range s.parentOwned {
		_ = f.Close()
	}
}
func (s *nativeStreams) finish() error {
	s.finishOnce.Do(func() { s.finishErr = s.drain() })
	return s.finishErr
}
func (s *nativeStreams) drain() error {
	// Child/job termination precedes this call, so inherited output handles can
	// no longer keep EOF open. Bound pipe drainage as with the Unix exec backend.
	if s.inputPipe != nil {
		_ = s.inputPipe.Close()
	}
	timer := time.NewTimer(3 * time.Second)
	defer timer.Stop()
	defer s.closeAll()
	var result error
	for range s.copies {
		select {
		case err := <-s.copied:
			result = errors.Join(result, err)
		case <-timer.C:
			return errors.Join(result, errors.New("command stream handling exceeded its deadline"))
		}
	}
	return result
}
