package cliui

import (
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	"github.com/jinyongp/devtools/internal/progress"
)

type ProgressOptions struct {
	Animate bool
	Width   int
}

type progressActivity struct {
	id      uint64
	message string
}

// Progress renders transient stderr feedback. Operation results remain owned by
// the normal document renderer; short activities produce no extra output.
type Progress struct {
	mu         sync.Mutex
	writer     io.Writer
	options    ProgressOptions
	activities []progressActivity
	suspended  map[uint64]bool
	started    time.Time
	visibleAt  time.Time
	lastID     uint64
	lastLine   time.Time
	frame      int
	painted    bool
	closed     bool
	failed     bool
	stop       chan struct{}
	done       chan struct{}
	closeOnce  sync.Once
}

func NewProgress(writer io.Writer, options ProgressOptions) *Progress {
	now := time.Now()
	p := &Progress{writer: writer, options: options, suspended: make(map[uint64]bool), started: now, visibleAt: now, stop: make(chan struct{}), done: make(chan struct{})}
	go p.run()
	return p
}

func (p *Progress) Observe(event progress.Event) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return
	}
	if event.Suspended {
		if event.Finished {
			delete(p.suspended, event.ID)
			if len(p.suspended) == 0 {
				// Short work following a prompt or child output stays quiet too.
				p.visibleAt = time.Now()
				p.lastID, p.lastLine = 0, time.Time{}
			}
		} else {
			p.suspended[event.ID] = true
			p.clear()
		}
		return
	}
	if event.Finished {
		for i, activity := range p.activities {
			if activity.id == event.ID {
				p.activities = append(p.activities[:i], p.activities[i+1:]...)
				break
			}
		}
		if len(p.activities) == 0 {
			p.clear()
		}
		return
	}
	p.activities = append(p.activities, progressActivity{id: event.ID, message: event.Message})
}

func (p *Progress) write(value string) {
	if p.failed {
		return
	}
	n, err := io.WriteString(p.writer, value)
	p.failed = err != nil || n != len(value)
}

func (p *Progress) clear() {
	if p.painted {
		p.write("\r\x1b[2K")
		p.painted = false
	}
}

func (p *Progress) render(now time.Time) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed || p.failed || len(p.suspended) > 0 || len(p.activities) == 0 {
		return
	}
	activity := p.activities[len(p.activities)-1]
	if now.Sub(p.visibleAt) < 250*time.Millisecond {
		return
	}
	if !p.options.Animate {
		if now.Sub(p.lastLine) < time.Second || p.lastID == activity.id && now.Sub(p.lastLine) < 10*time.Second {
			return
		}
	}
	message := strings.ReplaceAll(Escape(activity.message), "\n", "\\n")
	suffix := fmt.Sprintf(" (%s)", now.Sub(p.started).Round(time.Second))
	prefix := "... "
	if p.options.Animate {
		prefix = string("|/-\\"[p.frame%4]) + " "
		p.frame++
	}
	width := p.options.Width
	if width < 20 {
		width = 80
	}
	limit := width - cellWidth(prefix+suffix) - 1
	if cellWidth(message) > limit {
		var clipped strings.Builder
		for _, r := range message {
			if cellWidth(clipped.String()+string(r)) > limit-3 {
				break
			}
			clipped.WriteRune(r)
		}
		message = clipped.String() + "..."
	}
	line := prefix + message + suffix
	if p.options.Animate {
		p.write("\r\x1b[2K" + line)
		p.painted = true
	} else {
		p.write(line + "\n")
	}
	p.lastID, p.lastLine = activity.id, now
}

func (p *Progress) run() {
	defer close(p.done)
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case now := <-ticker.C:
			p.render(now)
		case <-p.stop:
			return
		}
	}
}

func (p *Progress) Close() {
	p.closeOnce.Do(func() {
		p.mu.Lock()
		p.closed = true
		p.clear()
		p.mu.Unlock()
		close(p.stop)
		<-p.done
	})
}
