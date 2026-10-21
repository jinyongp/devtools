package runnerengine

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jinyongp/devtools/internal/platformfs"
	"github.com/jinyongp/devtools/internal/valuehandoff"
	"github.com/jinyongp/devtools/internal/vault"
)

func TestMain(m *testing.M) {
	if len(os.Args) == 3 && os.Args[1] == "__vault-session" {
		if vault.ServeKeySession(context.Background(), os.Args[2]) != nil {
			os.Exit(91)
		}
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func TestEngineStartsBeforeLostACKAndReconcilesWithoutAnotherExecution(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	root, effects := filepath.Join(base, "data"), filepath.Join(base, "effects")
	guard, err := platformfs.OpenRoot(root, true)
	if err != nil {
		t.Fatal(err)
	}
	guard.Close()
	vid, _ := vault.NewID()
	pass := vault.NewMaterial([]byte("synthetic-preack-unlock"))
	defer pass.Destroy()
	master, err := (vault.KeyFileStore{Directory: filepath.Join(root, "vault")}).Initialize(ctx, vid, "passphrase", vault.PassphraseProtector{VaultID: vid, Passphrase: pass})
	if err != nil {
		t.Fatal(err)
	}
	defer master.Destroy()
	key, err := vault.NewKey()
	if err != nil {
		t.Fatal(err)
	}
	defer key.Destroy()
	p, err := vault.NewSealedProfile("preack", vid, master, key)
	if err != nil {
		t.Fatal(err)
	}
	for name, value := range map[string]string{"DEVTOOLS_REFERENCE_ENGINE_HELPER": "1", "DEVTOOLS_REFERENCE_EFFECTS": effects} {
		if _, err := p.Set(name, vault.Public, "", value, key); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := p.Set("TOKEN", vault.Secret, "", "synthetic-before-ack-secret", key); err != nil {
		t.Fatal(err)
	}
	files := vault.FileStore{Root: root, VaultID: vid, CatalogKey: master}
	if err := files.Initialize(ctx); err != nil {
		t.Fatal(err)
	}
	if err := files.Create(ctx, p, key); err != nil {
		t.Fatal(err)
	}
	client, err := (vault.SessionManager{Directory: filepath.Join(root, "vault")}).Client(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		retire, stop := context.WithTimeout(context.Background(), 5*time.Second)
		defer stop()
		if err := client.Retire(retire); err != nil {
			t.Error(err)
		}
	}()
	if err := client.Unlock(ctx, pass, false); err != nil {
		t.Fatal(err)
	}
	engine := newEngine(t, filepath.Join(base, "engine"))
	sendCtx, cancelSend := context.WithCancel(ctx)
	defer cancelSend()
	engine.hook = func(phase string) {
		if phase == "started" {
			cancelSend()
		}
	}
	runner, instance := "01234567-89ab-4cde-8fab-0123456789ab", "01234567-89ab-4cde-8fab-0123456789ac"
	receiverCtx, stopReceiver := context.WithCancel(ctx)
	done := make(chan error, 1)
	receiver := valuehandoff.Receiver{Root: root, RunnerID: runner, InstanceID: instance,
		Accept: func(ctx context.Context, input *valuehandoff.ReceivedInput) error {
			if err := engine.Accept(ctx, input); err != nil {
				return err
			}
			// A cooperating engine may start before ACK. Discarding both streams
			// keeps this fault fixture private while testing that exact gap.
			return engine.Handle(ctx, input, io.Discard, io.Discard)
		}, Handle: engine.Handle}
	go func() { done <- receiver.Serve(receiverCtx) }()
	defer func() {
		stopReceiver()
		select {
		case <-done:
		case <-time.After(6 * time.Second):
			t.Error("owned receiver did not stop")
		}
	}()
	s := valuehandoff.Store{Root: root, Profile: "preack"}
	id := func(last string) string { return "00000000-0000-4000-8000-0000000000" + last }
	deadline := time.Now().Add(5 * time.Second)
	for {
		_, e := s.Register(ctx, valuehandoff.RunnerInput{RunnerID: runner, InstanceID: instance, Protocol: 1, RedactionProtocol: 1}, id("01"))
		if e == nil {
			break
		}
		if e.Code != "runner_protocol_mismatch" || time.Now().After(deadline) {
			t.Fatal("receiver register", e)
		}
		time.Sleep(10 * time.Millisecond)
	}
	prepared, e := s.Prepare(ctx, valuehandoff.PrepareInput{ExternalExecutionID: "preack-job", Selection: valuehandoff.Selection{Keys: []string{"TOKEN", "DEVTOOLS_REFERENCE_ENGINE_HELPER", "DEVTOOLS_REFERENCE_EFFECTS"}}}, id("02"))
	if e != nil {
		t.Fatal(e)
	}
	approved, e := s.Approve(ctx, prepared.Item.ID, id("03"), valuehandoff.ApprovalInput{Revision: 1, RunnerID: runner, RunnerInstanceID: instance, ExternalExecutionID: prepared.Item.ExternalExecutionID, InputID: prepared.Item.InputID})
	if e != nil {
		t.Fatal(e)
	}
	if _, e := s.Deliver(sendCtx, prepared.Item.ID, id("04"), valuehandoff.RevisionInput{Revision: approved.Item.Revision}); e == nil || e.Code != "delivery_outcome_unknown" {
		t.Fatal("engine-before-ACK did not remain uncertain", e)
	}
	restarted := &Engine{Directory: engine.Directory, Program: engine.Program, Arguments: engine.Arguments}
	var fact Result
	for {
		fact, err = restarted.Lookup(ctx, "preack-job")
		if err == nil && fact.State == "finished" {
			break
		}
		if ctx.Err() != nil {
			t.Fatal("authoritative engine result unavailable")
		}
		time.Sleep(10 * time.Millisecond)
	}
	view, e := s.Status(ctx, prepared.Item.ID, "")
	if e != nil {
		t.Fatal(e)
	}
	reported, e := s.Report(ctx, prepared.Item.ID, id("05"), valuehandoff.ReportInput{Revision: view.Item.Revision, RunnerID: runner, ExternalExecutionID: fact.Job, InputID: fact.Input, State: "finished", ExternalRef: fact.Ref, ExitCode: fact.ExitCode})
	if e != nil || reported.Item.DeliveryState != "delivered" || reported.Item.ExecutionState != "reported_finished" {
		t.Fatal("engine lookup reconciliation", e)
	}
	if _, e := s.Deliver(ctx, prepared.Item.ID, id("04"), valuehandoff.RevisionInput{Revision: 2}); e == nil || e.Code != "delivery_outcome_unknown" {
		t.Fatal("completed original unknown changed", e)
	}
	if _, e := s.Deliver(ctx, prepared.Item.ID, id("06"), valuehandoff.RevisionInput{Revision: reported.Item.Revision}); e == nil || e.Code != "delivery_already_started" {
		t.Fatal("reconciliation authorized another send", e)
	}
	b, err := os.ReadFile(effects)
	if err != nil || string(b) != "effect\n" {
		t.Fatal("engine-before-ACK duplicated real helper effects", err)
	}
}
