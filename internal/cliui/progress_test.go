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

func TestProgressSuspendsOverlappingPromptsAndResumesAfterOutput(t *testing.T) {
	for _, animate := range []bool{false, true} {
		var out bytes.Buffer
		p := NewProgress(&out, ProgressOptions{Animate: animate})
		ctx := progress.WithObserver(context.Background(), p.Observe)
		finish := progress.Begin(ctx, "Preparing execution")
		p.render(time.Now().Add(time.Second))
		resume := progress.Suspend(ctx)
		if animate && !strings.HasSuffix(out.String(), "\r\x1b[2K") {
			t.Fatalf("prompt would overwrite feedback: %q", out.String())
		}
		other := progress.Suspend(ctx)
		nested := progress.Begin(ctx, "Checking preparation")
		before := out.Len()
		resume()
		p.render(time.Now().Add(3 * time.Second))
		if out.Len() != before {
			t.Fatalf("one prompt resumed another: %q", out.String())
		}
		other()
		p.render(time.Now())
		if out.Len() != before {
			t.Fatal("feedback flashed immediately after native output")
		}
		p.render(time.Now().Add(4 * time.Second))
		nested()
		finish()
		p.Close()
		if !strings.Contains(out.String(), "Checking preparation") {
			t.Fatalf("feedback did not resume: %q", out.String())
		}
	}
}
