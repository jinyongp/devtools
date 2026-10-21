package valuehandoff

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"testing"
	"time"

	"github.com/jinyongp/devtools/internal/vault"
)

func receiverFixture(t *testing.T, s Store, runner, instance string) {
	receiverConfigFixture(t, s, Receiver{Root: s.Root, RunnerID: runner, InstanceID: instance})
}
func receiverConfigFixture(t testing.TB, s Store, config Receiver) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- config.Serve(ctx) }()
	t.Cleanup(func() {
		cancel()
		select {
		case <-done:
		case <-time.After(6 * time.Second):
			t.Error("owned receiver did not stop")
		}
	})
	address, err := Address(s.Root, config.RunnerID, config.InstanceID)
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
		err = probeRunner(ctx, s.Root, address)
		cancel()
		if err == nil {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("owned receiver did not become ready", err)
		}
		time.Sleep(10 * time.Millisecond)
	}
}
func TestRunnerRegistrationProbeReplayAndInstanceFence(t *testing.T) {
	s, _ := fixture(t)
	ctx := context.Background()
	runner, instance := requestID(200), requestID(201)
	receiverFixture(t, s, runner, instance)
	input := RunnerInput{runner, instance, 1, 1}
	r, e := s.Register(ctx, input, requestID(202))
	if e != nil || !r.Changed || !r.Item.Enabled {
		t.Fatal("registration failed", e)
	}
	r, e = s.Register(ctx, input, requestID(202))
	if e != nil || !r.Replayed || !r.Changed {
		t.Fatal("registration replay changed outcome", e)
	}
	r, e = s.Register(ctx, input, requestID(203))
	if e != nil || r.Changed || r.Replayed {
		t.Fatal("identical registration not no-op", e)
	}
	other := Store{Root: s.Root, Profile: "other-profile"}
	got, e := other.Runner(ctx, runner)
	if e != nil || got.InstanceID != instance {
		t.Fatal("registration was profile scoped", e)
	}
	second := requestID(204)
	receiverFixture(t, s, runner, second)
	if _, e = s.Register(ctx, RunnerInput{runner, second, 1, 1}, requestID(205)); e == nil || e.Code != "runner_instance_conflict" {
		t.Fatal("live instance replaced without unregister", e)
	}
	if _, e = s.Unregister(ctx, runner, second, requestID(206)); e == nil || e.Code != "runner_instance_conflict" {
		t.Fatal("wrong observed instance disabled runner", e)
	}
	if _, e = s.Unregister(ctx, runner, instance, requestID(202)); e == nil || e.Code != "request_conflict" {
		t.Fatal("cross-action request reused", e)
	}
	if _, e = s.Unregister(ctx, runner, instance, requestID(207)); e != nil {
		t.Fatal(e)
	}
	if _, e = s.Register(ctx, RunnerInput{runner, second, 1, 1}, requestID(208)); e != nil {
		t.Fatal("disabled registration could not be replaced", e)
	}
	old, e := s.Unregister(ctx, runner, instance, requestID(207))
	if e != nil || !old.Replayed || old.Item.Enabled {
		t.Fatal("saved disable outcome changed", e)
	}
	current, e := s.Runner(ctx, runner)
	if e != nil || !current.Enabled || current.InstanceID != second {
		t.Fatal("replay disabled a later instance", e)
	}
	if _, err := vault.ListenPeer(ctx, s.Root, runner, instance); err == nil {
		t.Fatal("duplicate listener stole an active endpoint")
	}
}
func TestRunnerFrameLimitsAndClosedControlSchemas(t *testing.T) {
	var payloadLength [4]byte
	binary.BigEndian.PutUint32(payloadLength[:], maxPayloadFrame+1)
	if _, err := readFrameBytes(bytes.NewReader(payloadLength[:]), maxPayloadFrame); err != vault.ErrTooLarge {
		t.Fatal("payload limit checked after allocation", err)
	}
	for _, n := range []uint32{0, maxControlFrame + 1, 0xffffffff} {
		var b [4]byte
		binary.BigEndian.PutUint32(b[:], n)
		if _, err := readFrameBytes(bytes.NewReader(b[:]), maxControlFrame); err != vault.ErrTooLarge {
			t.Fatal("oversized length not rejected before body read", n, err)
		}
	}
	for _, body := range []string{`{"protocol":1,"protocol":1}`, `{"protocol":null}`, `{"Protocol":1}`, `{"protocol":1,"unknown":"x"}`, `{} {}`, string([]byte{'"', 0xff, '"'})} {
		var h helloFrame
		if DecodeControl([]byte(body), &h) == nil {
			t.Fatal("ambiguous frame accepted")
		}
	}
	s, _ := fixture(t)
	runner, instance := requestID(300), requestID(301)
	receiverFixture(t, s, runner, instance)
	address, err := Address(s.Root, runner, instance)
	if err != nil {
		t.Fatal(err)
	}
	wrong := address
	wrong.RootID = "0000000000000000000000000000000000000000000000000000000000000000"
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err = probeRunner(ctx, s.Root, wrong); err == nil {
		t.Fatal("another root accepted")
	}
	c, h, challenge, err := connectRunner(ctx, s.Root, address)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	p := probeFrame{1, "probe", address.RootID, address.VaultID, runner, instance, h.ServerChallenge, challenge}
	p.InstanceID = requestID(302)
	if err = writeFrame(c, p, maxControlFrame); err != nil {
		t.Fatal(err)
	}
	var response readyFrame
	if err = readControl(c, &response); err == nil {
		t.Fatal("mismatched instance accepted")
	}
	encoded, _ := json.Marshal(address)
	if bytes.Contains(encoded, []byte("ticket")) {
		t.Fatal("address leaked session authority")
	}
}

func TestRunnerRejectsOldProtocolOrWrongVaultBeforeSending(t *testing.T) {
	s, _ := fixture(t)
	for i := 0; i < 2; i++ {
		func() {
			runner, instance := requestID(400+i*2), requestID(401+i*2)
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			address, err := Address(s.Root, runner, instance)
			if err != nil {
				t.Fatal(err)
			}
			listener, err := vault.ListenPeer(ctx, s.Root, runner, instance)
			if err != nil {
				t.Fatal(err)
			}
			defer listener.Close()
			done := make(chan error, 1)
			go func() {
				c, err := listener.Accept(ctx)
				if err != nil {
					done <- err
					return
				}
				defer c.Close()
				c.SetDeadline(time.Now().Add(time.Second))
				challenge, _ := vault.NewID()
				h := helloFrame{1, "hello", address.RootID, address.VaultID, runner, instance, challenge}
				if i == 0 {
					h.Protocol = 3
				} else {
					h.VaultID = "00000000000000000000000000000000"
				}
				if err = writeFrame(c, h, maxControlFrame); err != nil {
					done <- err
					return
				}
				var b [1]byte
				n, err := c.Read(b[:])
				if n != 0 {
					t.Error("client sent bytes before validating the receiver")
				}
				done <- err
			}()
			if err = probeRunner(ctx, s.Root, address); err == nil {
				t.Fatal("invalid peer accepted")
			}
			<-done
		}()
	}
}
