package valuehandoff

import (
	"context"
	"encoding/json"
	"io"
	"sync"
	"time"

	"github.com/jinyongp/devtools/internal/redact"
	"github.com/jinyongp/devtools/internal/vault"
)

// ServeRunner hosts probes only until the caller configures its execution engine.
func ServeRunner(ctx context.Context, root, runner, instance string) error {
	return (Receiver{Root: root, RunnerID: runner, InstanceID: instance}).Serve(ctx)
}

type InputIdentity struct{ ExecutionID, InputID, ExternalExecutionID, DeliveryID string }
type Receiver struct {
	Root, RunnerID, InstanceID string
	Stdout, Stderr             io.Writer
	Admit                      func(context.Context, InputIdentity) error
	Accept                     func(context.Context, *ReceivedInput) error
	Handle                     func(context.Context, *ReceivedInput, io.Writer, io.Writer) error
	beforeACK                  func(vault.PeerConnection)
}

func (r Receiver) Serve(ctx context.Context) error {
	canonical, err := CanonicalRoot(r.Root)
	if err != nil {
		return err
	}
	r.Root = canonical
	address, err := Address(r.Root, r.RunnerID, r.InstanceID)
	if err != nil {
		return err
	}
	listener, err := vault.ListenPeer(ctx, r.Root, r.RunnerID, r.InstanceID)
	if err != nil {
		return err
	}
	defer listener.Close()
	var wg sync.WaitGroup
	defer wg.Wait()
	stop := context.AfterFunc(ctx, func() { listener.Close() })
	defer stop()
	capacity := make(chan struct{}, 8)
	for {
		select {
		case capacity <- struct{}{}:
		case <-ctx.Done():
			return ctx.Err()
		}
		c, err := listener.Accept(ctx)
		if err != nil {
			<-capacity
			return err
		}
		wg.Add(1)
		go func() {
			defer func() { _ = recover() }()
			defer wg.Done()
			defer func() { <-capacity }()
			defer c.Close()
			c.SetDeadline(time.Now().Add(5 * time.Second))
			stop := context.AfterFunc(ctx, func() { c.Close() })
			defer stop()
			server, err := vault.NewID()
			if err != nil {
				return
			}
			h := helloFrame{1, "hello", address.RootID, address.VaultID, r.RunnerID, r.InstanceID, server}
			if writeFrame(c, h, maxControlFrame) != nil {
				return
			}
			body, err := readFrameBytes(c, maxControlFrame)
			if err != nil {
				return
			}
			defer clear(body)
			var kind struct {
				Type string `json:"type"`
			}
			if json.Unmarshal(body, &kind) != nil {
				return
			}
			if kind.Type == "probe" {
				var probe probeFrame
				if DecodeControl(body, &probe) != nil || probe.Protocol != 1 || probe.RootID != address.RootID || probe.VaultID != address.VaultID || probe.RunnerID != r.RunnerID || probe.InstanceID != r.InstanceID || probe.ServerChallenge != server || !idRE.MatchString(probe.ClientChallenge) {
					return
				}
				_ = writeFrame(c, readyFrame{1, "registered", server, probe.ClientChallenge, 1}, maxControlFrame)
				return
			}
			var bind bindFrame
			if DecodeControl(body, &bind) != nil || bind.Protocol != 1 || bind.Type != "bind" || bind.RootID != address.RootID || bind.VaultID != address.VaultID || bind.RunnerID != r.RunnerID || bind.InstanceID != r.InstanceID || bind.ServerChallenge != server || !idRE.MatchString(bind.ClientChallenge) || !idRE.MatchString(bind.ExecutionID) || !idRE.MatchString(bind.InputID) || !opaque(bind.ExternalExecutionID) {
				return
			}
			identity := InputIdentity{bind.ExecutionID, bind.InputID, bind.ExternalExecutionID, ""}
			if r.Handle == nil {
				return
			}
			if r.Admit != nil && r.Admit(ctx, identity) != nil {
				return
			}
			if writeFrame(c, readyFrame{1, "ready", server, bind.ClientChallenge, 1}, maxControlFrame) != nil {
				return
			}
			encoded, err := readFrameBytes(c, maxPayloadFrame)
			if err != nil {
				return
			}
			defer clear(encoded)
			payload, err := decodePayload(encoded)
			if err != nil {
				return
			}
			defer payload.destroy()
			if payload.ExecutionID != bind.ExecutionID || payload.InputID != bind.InputID || payload.ExternalExecutionID != bind.ExternalExecutionID {
				return
			}
			input := &ReceivedInput{payload: payload}
			plan, _ := redact.Resolve(redact.Policy{Mode: redact.Redacted}, false, false, false)
			streams, err := redact.Protect(ctx, payload.Redaction.Patterns, plan, r.Stdout, r.Stderr)
			if err != nil {
				return
			}
			abort := true
			defer func() { streams.Close(abort) }()
			if r.Accept != nil && r.Accept(streams.Context, input) != nil {
				return
			}
			if r.beforeACK != nil {
				r.beforeACK(c)
			}
			// ACK confirms acceptance and installed masking, never app start/use.
			_ = writeFrame(c, acceptedFrame{1, "accepted", server, bind.ClientChallenge, payload.ExecutionID, payload.InputID, payload.DeliveryID, 1}, maxControlFrame)
			abort = r.Handle(streams.Context, input, streams.Out, streams.Err) != nil
		}()
	}
}
