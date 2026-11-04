package cliui

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/jinyongp/devtools/internal/progress"
)

func TestProgressRestoresEnclosingActivityAndClearsBeforeResult(t *testing.T) {
	var out bytes.Buffer
	p := NewProgress(&out, ProgressOptions{Animate: true, Width: 110})
	ctx := progress.WithObserver(context.Background(), p.Observe)
	finish := progress.Begin(ctx, "Upgrading storage")
	finishWait := progress.Begin(ctx, "Waiting for storage access\n\x1b[2J")
	p.render(time.Now().Add(2 * time.Second))
	finishWait()
	p.render(time.Now().Add(3 * time.Second))
	finish()
	p.Close()
	p.Close()
	output := out.String()
	if !strings.Contains(output, "Waiting for storage access\\n\\u001b[2J") || !strings.Contains(output, "Upgrading storage") || !strings.HasSuffix(output, "\r\x1b[2K") || strings.Contains(output, "\x1b[2J") {
		t.Fatalf("unsafe or unrestored progress: %q", output)
	}
}

func TestPlainProgressThrottlesHeartbeatsWithoutTerminalControls(t *testing.T) {
	var out bytes.Buffer
	p := NewProgress(&out, ProgressOptions{})
	finish := progress.Begin(progress.WithObserver(context.Background(), p.Observe), "Waiting for another storage upgrade")
	now := time.Now().Add(2 * time.Second)
	p.render(now)
	p.render(now.Add(time.Second))
	p.render(now.Add(11 * time.Second))
	finish()
	p.Close()
	output := out.String()
	if strings.Count(output, "Waiting for another storage upgrade") != 2 || strings.ContainsAny(output, "\r\x1b") || !strings.Contains(output, "(2s)") {
		t.Fatalf("invalid plain progress: %q", output)
	}
}
