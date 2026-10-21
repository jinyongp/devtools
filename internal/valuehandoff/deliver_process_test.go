package valuehandoff

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"sync/atomic"
	"testing"
	"time"
)

type deliveryFixtureInput struct {
	Root, ID, RequestID, Phase string
	Revision                   uint64
}

func runDeliveryFixture() int {
	var input deliveryFixtureInput
	reader := bufio.NewReader(io.LimitReader(os.Stdin, 65536))
	body, err := reader.ReadBytes('\n')
	if err != nil || json.Unmarshal(body, &input) != nil {
		return 91
	}
	s := Store{Root: input.Root, Profile: "app", deliveryHook: func(phase string) {
		if phase == input.Phase {
			io.WriteString(os.Stdout, "checkpoint\n")
			var b [1]byte
			io.ReadFull(reader, b[:])
		}
	}}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if _, e := s.Deliver(ctx, input.ID, input.RequestID, RevisionInput{input.Revision}); e != nil {
		return 92
	}
	return 0
}
func TestRealSenderDeathAfterIntentOrACKNeverResends(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for i, phase := range []string{"intent", "finalize"} {
		t.Run(phase, func(t *testing.T) {
			var count atomic.Int32
			s, _, approved := deliveryFixture(t, Receiver{Handle: func(context.Context, *ReceivedInput, io.Writer, io.Writer) error { count.Add(1); return nil }})
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, executable, "__handoff-deliver-fixture")
			stdin, err := cmd.StdinPipe()
			if err != nil {
				t.Fatal(err)
			}
			defer stdin.Close()
			stdout, err := cmd.StdoutPipe()
			if err != nil {
				t.Fatal(err)
			}
			if err = cmd.Start(); err != nil {
				t.Fatal(err)
			}
			waited := false
			defer func() {
				if !waited {
					cmd.Process.Kill()
					cmd.Wait()
				}
			}()
			id := requestID(700 + i)
			if err = json.NewEncoder(stdin).Encode(deliveryFixtureInput{s.Root, approved.Item.ID, id, phase, 2}); err != nil {
				t.Fatal(err)
			}
			line, err := bufio.NewReader(stdout).ReadString('\n')
			if err != nil || line != "checkpoint\n" {
				t.Fatal("owned sender did not reach durable checkpoint", err)
			}
			if err = cmd.Process.Kill(); err != nil {
				t.Fatal(err)
			}
			if err = cmd.Wait(); err == nil {
				t.Fatal("owned sender was not killed")
			}
			waited = true
			want := int32(0)
			if phase == "finalize" {
				want = 1
				waitCount(t, &count, want)
			}
			view, e := s.Status(ctx, approved.Item.ID, "")
			if e != nil || view.Item.DeliveryState != "unknown" {
				t.Fatal("abandoned intent not unknown", e)
			}
			for _, request := range []string{id, requestID(710 + i)} {
				if _, e = s.Deliver(ctx, approved.Item.ID, request, RevisionInput{2}); e == nil || e.Code != "delivery_outcome_unknown" {
					t.Fatal("crashed sender restarted sensitive send", e)
				}
			}
			if count.Load() != want {
				t.Fatal("retry increased receiver acceptance count")
			}
		})
	}
}
