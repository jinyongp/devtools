package valuehandoff

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"sync"
	"testing"

	"github.com/jinyongp/devtools/internal/protocol"
	"github.com/jinyongp/devtools/internal/redact"
	"github.com/jinyongp/devtools/internal/values"
	"github.com/jinyongp/devtools/internal/vault"
	"path/filepath"
)

func TestReceiverKeepsOldPatternsThroughLockUntilOutputEOF(t *testing.T) {
	ready, release, ended := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	defer unblock()
	var out, diagnostic safeBuffer
	s, c, r := deliveryFixture(t, Receiver{Stdout: &out, Stderr: &diagnostic, Handle: func(ctx context.Context, input *ReceivedInput, stdout, stderr io.Writer) error {
		close(ready)
		<-release
		defer close(ended)
		for _, stream := range []io.Writer{stdout, stderr} {
			io.WriteString(stream, "synthetic-handoff-")
			io.WriteString(stream, "canary-TOKEN\nEOF")
		}
		return nil
	}})
	ctx := context.Background()
	if _, e := s.Deliver(ctx, r.Item.ID, requestID(810), RevisionInput{2}); e != nil {
		t.Fatal(e)
	}
	<-ready
	store := values.CurrentStore(filepath.Join(s.Root, "profiles"), s.Profile)
	if _, e := store.Update(ctx, func(ed *values.Editor) (bool, *protocol.Error) {
		return ed.Set(values.Secret, "TOKEN", "", "replacement-secret")
	}); e != nil {
		t.Fatal(e)
	}
	if err := c.Lock(ctx); err != nil {
		t.Fatal(err)
	}
	unblock()
	<-ended
	// Protect closes after Handle returns; its trailing prefix may still be
	// buffered, but neither stream may reveal the old full secret or its chunks.
	for _, b := range []*safeBuffer{&out, &diagnostic} {
		if strings.Contains(b.String(), "synthetic-handoff") || !strings.Contains(b.String(), redact.Replacement) {
			t.Fatal("old pattern lost before EOF")
		}
	}
}

func TestPayloadPatternBudgetHasNoRawFallback(t *testing.T) {
	p := privatePayload{Protocol: 1, Type: "payload", ExecutionID: strings.Repeat("a", 32), InputID: strings.Repeat("b", 32), DeliveryID: strings.Repeat("c", 32), ExternalExecutionID: "budget-job", Values: []storedValue{{Key: "TOKEN", Kind: vault.Secret, Value: strings.Repeat("x", redact.MaxPatternBytes+1)}}}
	p.Redaction = privateRedaction{Protocol: 1, Patterns: patternsFor(p.Values)}
	defer p.destroy()
	if _, err := json.Marshal(p.Redaction); err == nil || fmt.Sprintf("%+v", p.Redaction) != "[REDACTED]" {
		t.Fatal("private patterns allowed ordinary serialization")
	}
	if _, err := encodePayload(p); err != vault.ErrTooLarge {
		t.Fatal("oversized masking set accepted", err)
	}
}
