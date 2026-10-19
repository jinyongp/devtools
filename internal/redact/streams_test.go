package redact

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"testing"
	"time"
)

func TestPolicyPerStreamTTYAndCapture(t *testing.T) {
	for _, tc := range []struct {
		policy                  Policy
		outTTY, errTTY, capture bool
		out, diagnostic         Mode
		reason                  string
	}{
		{Policy{}, false, false, false, Redacted, Redacted, "non_tty"},
		{Policy{}, true, false, false, Raw, Redacted, "tty"},
		{Policy{}, false, true, false, Redacted, Raw, "non_tty"},
		{Policy{Mode: Redacted}, true, true, false, Redacted, Redacted, "explicit"},
		{Policy{Stdout: Raw}, false, false, false, Raw, Redacted, "explicit"},
		{Policy{Mode: Raw}, true, false, true, Redacted, Redacted, "capture"},
	} {
		plan, err := Resolve(tc.policy, tc.outTTY, tc.errTTY, tc.capture)
		if err != nil || !plan.Valid() || plan.Stdout != tc.out || plan.Stderr != tc.diagnostic || plan.StdoutReason != tc.reason {
			t.Fatal("wrong stream policy", err, plan)
		}
	}
	if _, err := Resolve(Policy{Stderr: "binary"}, false, false, false); err == nil {
		t.Fatal("unknown mode accepted")
	}
}
func TestBinaryStdoutExceptionKeepsStderrProtected(t *testing.T) {
	plan, _ := Resolve(Policy{Stdout: Raw}, false, false, false)
	var out, diagnostic bytes.Buffer
	streams, err := Protect(context.Background(), [][]byte{[]byte("TOKEN")}, plan, &out, &diagnostic)
	if err != nil {
		t.Fatal(err)
	}
	binary := []byte{0, 0xff, 'T', 'O', 'K', 'E', 'N', 0x80}
	if _, err = streams.Out.Write(binary); err != nil {
		t.Fatal(err)
	}
	streams.Err.Write([]byte("TO"))
	streams.Err.Write([]byte("KEN"))
	if err = streams.Close(false); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(out.Bytes(), binary) || diagnostic.String() != "[REDACTED]" {
		t.Fatal("raw exception affected another stream")
	}
}
func TestStreamFailureCancelsExecutionAndNeverRetries(t *testing.T) {
	plan, _ := Resolve(Policy{}, false, false, false)
	sink := &failedSink{}
	streams, err := Protect(context.Background(), [][]byte{[]byte("TOKEN")}, plan, sink, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = streams.Out.Write([]byte("TOKEN")); !errors.Is(err, io.ErrClosedPipe) {
		t.Fatal(err)
	}
	select {
	case <-streams.Context.Done():
	case <-time.After(time.Second):
		t.Fatal("failed output did not cancel child")
	}
	if err = streams.Close(true); !errors.Is(err, ErrOutput) {
		t.Fatal(err)
	}
	if sink.writes != 1 {
		t.Fatal("output failure retried")
	}
}
func TestNoFlushOnCancellationAndNoRawLimitFallback(t *testing.T) {
	plan, _ := Resolve(Policy{}, false, false, false)
	var out bytes.Buffer
	streams, err := Protect(context.Background(), [][]byte{[]byte("TOKEN")}, plan, &out, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	streams.Out.Write([]byte("TOK"))
	if err = streams.Close(true); err != nil {
		t.Fatal(err)
	}
	if out.Len() != 0 {
		t.Fatal("cancellation flushed an unresolved prefix")
	}
	if _, err = Protect(context.Background(), [][]byte{bytes.Repeat([]byte{'a'}, MaxPatternBytes+1)}, plan, &out, io.Discard); !errors.Is(err, ErrLimit) {
		t.Fatal("limit became raw pass-through", err)
	}
}

func TestRealFileBackpressureCancellationReleasesProducer(t *testing.T) {
	read, write, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer read.Close()
	defer write.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	plan, _ := Resolve(Policy{}, false, false, false)
	streams, err := Protect(ctx, [][]byte{[]byte("TOKEN")}, plan, write, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { _, err := streams.Out.Write(bytes.Repeat([]byte("TOKEN"), 100000)); done <- err }()
	// A pipe has a real finite kernel buffer. Wait for the filtered sink's first
	// write to own a block, while intentionally providing no consumer reads.
	deadline := time.Now().Add(time.Second)
	for len(streams.fileSinks[0].queue) == 0 {
		if time.Now().After(deadline) {
			t.Fatal("file sink never received output")
		}
		time.Sleep(time.Millisecond)
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("producer waited behind blocked file destination")
	}
	if err := streams.Close(true); err != nil {
		t.Fatal("cancellation became output failure", err)
	}
	if len(streams.matcher.states) != 0 {
		t.Fatal("filtered file callback retained raw patterns")
	}
	write.Close() // The test owns this pipe and can release the kernel write.
	select {
	case <-streams.fileSinks[0].done:
	case <-time.After(time.Second):
		t.Fatal("owned file sink did not finish")
	}
	output, err := io.ReadAll(read)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(output, []byte("TOKEN")) {
		t.Fatal("unfiltered bytes reached OS pipe")
	}
}
