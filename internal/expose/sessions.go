package expose

import (
	"bufio"
	"context"
	"net"
	"net/http"
	"sync"
	"time"
)

type Sessions struct {
	mu          sync.Mutex
	revoked     bool
	next        int
	requests    map[int]context.CancelFunc
	connections map[net.Conn]bool
	changed     chan struct{}
}

func (s *Sessions) signal() {
	if s.changed != nil {
		close(s.changed)
	}
	s.changed = make(chan struct{})
}
func (s *Sessions) Begin(w http.ResponseWriter, r *http.Request) (http.ResponseWriter, *http.Request, func(), bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.revoked {
		return w, r, func() {}, false
	}
	if s.requests == nil {
		s.requests = map[int]context.CancelFunc{}
		s.connections = map[net.Conn]bool{}
	}
	s.next++
	id := s.next
	ctx, cancel := context.WithCancel(r.Context())
	s.requests[id] = cancel
	return &sessionWriter{ResponseWriter: w, s: s}, r.WithContext(ctx), func() { cancel(); s.mu.Lock(); delete(s.requests, id); s.signal(); s.mu.Unlock() }, true
}

// Close cancels HTTP/2 streams independently and closes only hijacked sockets
// belonging to this exposure. It never closes a shared frontend connection.
func (s *Sessions) Close(ctx context.Context) bool {
	s.mu.Lock()
	s.revoked = true
	for _, cancel := range s.requests {
		cancel()
	}
	connections := []net.Conn{}
	for c := range s.connections {
		connections = append(connections, c)
	}
	s.mu.Unlock()
	for _, c := range connections {
		_ = c.Close()
	}
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	for {
		s.mu.Lock()
		if len(s.requests) == 0 && len(s.connections) == 0 {
			s.mu.Unlock()
			return true
		}
		if s.changed == nil {
			s.changed = make(chan struct{})
		}
		changed := s.changed
		s.mu.Unlock()
		select {
		case <-ctx.Done():
			return false
		case <-deadline.C:
			return false
		case <-changed:
		}
	}
}

type sessionWriter struct {
	http.ResponseWriter
	s *Sessions
}

func (w *sessionWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }
func (w *sessionWriter) Flush()                      { _ = http.NewResponseController(w.ResponseWriter).Flush() }
func (w *sessionWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	c, rw, e := http.NewResponseController(w.ResponseWriter).Hijack()
	if e != nil {
		return nil, nil, e
	}
	w.s.mu.Lock()
	if w.s.revoked {
		w.s.mu.Unlock()
		c.Close()
		return nil, nil, net.ErrClosed
	}
	t := &sessionConn{Conn: c, s: w.s}
	w.s.connections[t] = true
	w.s.mu.Unlock()
	return t, rw, nil
}

type sessionConn struct {
	net.Conn
	s    *Sessions
	once sync.Once
}

func (c *sessionConn) Close() error {
	e := c.Conn.Close()
	c.once.Do(func() { c.s.mu.Lock(); delete(c.s.connections, c); c.s.signal(); c.s.mu.Unlock() })
	return e
}
