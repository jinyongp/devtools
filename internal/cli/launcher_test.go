package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/jinyongp/devtools/internal/execution"
	"github.com/jinyongp/devtools/internal/network"
	"github.com/jinyongp/devtools/internal/process"
	"github.com/jinyongp/devtools/internal/project"
	"github.com/jinyongp/devtools/internal/protocol"
	"github.com/jinyongp/devtools/internal/services"
	"github.com/jinyongp/devtools/internal/tasks"
	"github.com/jinyongp/devtools/internal/values"
	"github.com/jinyongp/devtools/internal/vault"
)

func TestMain(m *testing.M) {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "__process-launch":
			if len(os.Args) != 5 {
				os.Exit(90)
			}
			lease, err := services.OpenInheritedLease(os.Args[4])
			if err != nil {
				os.Exit(91)
			}
			ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM, syscall.SIGHUP)
			defer stop()
			if ServePreparedProcessWithLease(ctx, os.Args[2], os.Args[3], lease) != nil {
				os.Exit(92)
			}
			os.Exit(0)
		case "__vault-session":
			if len(os.Args) != 3 || vault.ServeKeySession(context.Background(), os.Args[2]) != nil {
				os.Exit(93)
			}
			os.Exit(0)
		case "__launcher-test-app", "__launcher-test-once":
			if len(os.Args) != 3 {
				os.Exit(94)
			}
			b, _ := json.Marshal(map[string]string{"token": os.Getenv("TOKEN"), "task_context": os.Getenv("DEVTOOLS_TASK_CONTEXT")})
			if os.WriteFile(os.Args[2], b, 0600) != nil {
				os.Exit(95)
			}
			if os.Args[1] == "__launcher-test-once" {
				os.Exit(0)
			}
			signals := make(chan os.Signal, 1)
			signal.Notify(signals, os.Interrupt, syscall.SIGTERM)
			fmt.Fprintln(os.Stdout, "synthetic launcher child running")
			<-signals
			control := filepath.Dir(os.Args[2])
			if _, err := os.Stat(filepath.Join(control, "pause-stop")); err == nil {
				os.WriteFile(filepath.Join(control, "stop-entered"), []byte("stopping"), 0600)
				for {
					if _, err := os.Stat(filepath.Join(control, "release-stop")); err == nil {
						break
					}
					time.Sleep(time.Millisecond)
				}
			}
			os.Exit(0)
		}
	}
	os.Exit(m.Run())
}

func TestFrozenLauncherRealProcessRestartAndLock(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	data, root := privateTempDir(t), privateTempDir(t)
	exe, _ := os.Executable()
	output := filepath.Join(root, "observed.json")
	args, _ := json.Marshal([]string{exe, "__launcher-test-app", output})
	config := "profile='launcher-fixture'\n[commands.web]\ninject=true\nexec=" + string(args) + "\n"
	if err := os.WriteFile(filepath.Join(root, "devtools.toml"), []byte(config), 0600); err != nil {
		t.Fatal(err)
	}
	vaultID, _ := vault.NewID()
	dir := filepath.Join(data, "vault")
	pass := vault.NewMaterial([]byte("synthetic isolated launcher fixture passphrase"))
	defer pass.Destroy()
	protector := vault.PassphraseProtector{VaultID: vaultID, Passphrase: pass}
	master, err := (vault.KeyFileStore{Directory: dir}).Initialize(ctx, vaultID, "passphrase", protector)
	if err != nil {
		t.Fatal(err)
	}
	defer master.Destroy()
	keyset, err := vault.NewKey()
	if err != nil {
		t.Fatal(err)
	}
	defer keyset.Destroy()
	// Create an authenticated disk profile, not a mock value provider.
	store := vault.FileStore{Root: data, VaultID: vaultID, CatalogKey: master}
	profile, err := vault.NewSealedProfile("launcher-fixture", vaultID, master, keyset)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = profile.Set("TOKEN", vault.Secret, "", "frozen-one", keyset); err != nil {
		t.Fatal(err)
	}
	if err = store.Initialize(ctx); err != nil {
		t.Fatal(err)
	}
	if err = store.Create(ctx, profile, keyset); err != nil {
		t.Fatal(err)
	}
	client, err := (vault.SessionManager{Directory: dir}).Client(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		client.Retire(ctx)
	})
	if err = client.Unlock(ctx, pass, false); err != nil {
		t.Fatal(err)
	}
	beforeReturn := func() {}
	service := services.Store{Data: data}
	service.PrepareInput = func(ctx context.Context, r services.Record, _ *network.ExecutionSnapshot) (execution.Prepared, *protocol.Error) {
		p, e := project.Resolve(r.Directory, "")
		if e != nil {
			return execution.Prepared{}, e
		}
		command, e := execution.ResolveConfigured(p, r.Command, r.EnvOverride, nil)
		if e != nil {
			return execution.Prepared{}, e
		}
		dependencies := execution.Dependencies{VaultDirectory: dir, ValueSnapshot: func(ctx context.Context, profile, env string, inject bool) (*values.Snapshot, *protocol.Error) {
			return values.CaptureVaultSession(ctx, data, profile, env, inject, time.Now(), client)
		}}
		prepared, e := execution.PrepareInput(ctx, command, dependencies, append(os.Environ(), "DEVTOOLS_TASK_CONTEXT=synthetic-do-not-inherit"), execution.PlanOptions{Parent: process.ParentPolicy{}})
		if e == nil {
			beforeReturn()
		}
		return prepared, e
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
		defer cancel()
		records, _ := service.List(ctx, "")
		for _, r := range records {
			if r.EndedAt == nil {
				service.Apply(ctx, services.Request{Action: "stop", ID: r.ID, RequestID: tasks.ID()})
			}
		}
	})
	first, e := service.Apply(ctx, services.Request{Action: "start", Directory: root, Command: "web", RequestID: tasks.ID()})
	if e != nil || first.Item.State != "running" {
		t.Fatalf("start failed: %v state=%s reason=%s", e, first.Item.State, first.Item.Reason)
	}
	waitValue := func(want string) {
		t.Helper()
		deadline := time.Now().Add(3 * time.Second)
		for time.Now().Before(deadline) {
			b, err := os.ReadFile(output)
			var result map[string]string
			if err == nil && json.Unmarshal(b, &result) == nil && result["token"] == want && result["task_context"] == "" {
				return
			}
			time.Sleep(5 * time.Millisecond)
		}
		t.Fatal("actual child did not observe frozen input")
	}
	waitValue("frozen-one")
	beforeReturn = func() {
		if err := client.Lock(ctx); err != nil {
			t.Fatal(err)
		}
	}
	rejected, e := service.Apply(ctx, services.Request{Action: "restart", ID: first.Item.ID, RequestID: tasks.ID()})
	if e == nil {
		t.Fatal("locked frozen input was admitted", rejected.Item.State)
	}
	current, e := service.Status(ctx, first.Item.ID)
	if e != nil || current.State != "running" {
		t.Fatal("lock-before-admit stopped old execution", e, current.State)
	}
	if err = client.Unlock(ctx, pass, false); err != nil {
		t.Fatal(err)
	}
	beforeReturn = func() {
		// Mutate both disk values and config after snapshot authentication. The
		// particular launcher must still start its captured input and executable.
		if _, err := store.Update(ctx, "launcher-fixture", keyset, func(p *vault.Profile) (bool, error) { return p.Set("TOKEN", vault.Secret, "", "newer-value", keyset) }); err != nil {
			t.Fatal(err)
		}
		changed := strings.Replace(config, string(args), "['/definitely-missing-executable']", 1)
		if err := os.WriteFile(filepath.Join(root, "devtools.toml"), []byte(changed), 0600); err != nil {
			t.Fatal(err)
		}
	}
	request := services.Request{Action: "restart", ID: first.Item.ID, RequestID: tasks.ID()}
	if err := os.Remove(output); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "pause-stop"), []byte("pause"), 0600); err != nil {
		t.Fatal(err)
	}
	type outcome struct {
		result services.Result
		err    *protocol.Error
	}
	completed := make(chan outcome, 1)
	go func() { r, e := service.Apply(ctx, request); completed <- outcome{r, e} }()
	deadline := time.Now().Add(3 * time.Second)
	for {
		if _, err := os.Stat(filepath.Join(root, "stop-entered")); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("old stop boundary not reached")
		}
		time.Sleep(time.Millisecond)
	}
	locked := make(chan error, 1)
	go func() { locked <- client.Lock(ctx) }()
	for {
		status, err := client.Status(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if status.State == vault.LockedManual {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("lock intent not persisted")
		}
		time.Sleep(time.Millisecond)
	}
	select {
	case err := <-locked:
		t.Fatal("lock acknowledged before admitted spawn outcome", err)
	default:
	}
	if err := os.WriteFile(filepath.Join(root, "release-stop"), []byte("release"), 0600); err != nil {
		t.Fatal(err)
	}
	var result outcome
	select {
	case result = <-completed:
	case <-time.After(5 * time.Second):
		t.Fatal("admitted restart did not finish")
	}
	next, e := result.result, result.err
	if e != nil || next.Item.State != "running" {
		t.Fatalf("frozen restart failed: %v state=%s reason=%s", e, next.Item.State, next.Item.Reason)
	}
	select {
	case err := <-locked:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("lock did not drain known spawn")
	}
	waitValue("frozen-one")
	replay, e := service.Apply(ctx, request)
	if e != nil || !replay.Replayed || replay.Item.ID != next.Item.ID {
		t.Fatal("same UUID launched again", e)
	}

	if err = client.Lock(ctx); err != nil {
		t.Fatal("spawn outcome did not release admission", err)
	}
	// The foreground CLI process owns its admission directly. It uses the same
	// frozen ticket and must retire it at Start, rather than on command exit.
	foregroundFile := filepath.Join(root, "foreground.json")
	foregroundCommand := execution.Command{Project: project.Context{Profile: "launcher-fixture"}, Directory: root, Exec: []string{exe, "__launcher-test-once", foregroundFile}, Inject: true}
	dependencies := execution.Dependencies{VaultDirectory: dir, ValueSnapshot: func(ctx context.Context, profile, env string, inject bool) (*values.Snapshot, *protocol.Error) {
		return values.CaptureVaultSession(ctx, data, profile, env, inject, time.Now(), client)
	}}
	if err = client.Unlock(ctx, pass, false); err != nil {
		t.Fatal(err)
	}
	blocked, prepareErr := execution.PrepareInput(ctx, foregroundCommand, dependencies, os.Environ(), execution.PlanOptions{})
	if prepareErr != nil {
		t.Fatal(prepareErr)
	}
	if err = client.Lock(ctx); err != nil {
		t.Fatal(err)
	}
	_, executeErr := blocked.Execute(ctx, nil, nil, nil, io.Discard, io.Discard)
	blocked.Release()
	if executeErr == nil {
		t.Fatal("foreground ignored lock-before-admit")
	}
	if _, err = os.Stat(foregroundFile); !os.IsNotExist(err) {
		t.Fatal("blocked foreground child ran", err)
	}
	if err = client.Unlock(ctx, pass, false); err != nil {
		t.Fatal(err)
	}
	foreground, prepareErr := execution.PrepareInput(ctx, foregroundCommand, dependencies, os.Environ(), execution.PlanOptions{})
	if prepareErr != nil {
		t.Fatal(prepareErr)
	}
	defer foreground.Release()
	if code, e := foreground.Execute(ctx, nil, nil, nil, io.Discard, io.Discard); e != nil || code != 0 {
		t.Fatal("foreground launch", e, code)
	}
	if _, e := foreground.Execute(ctx, nil, nil, nil, io.Discard, io.Discard); e == nil || e.Code != "input_snapshot_consumed" {
		t.Fatal("same input reused", e)
	}
	if err = client.Lock(ctx); err != nil {
		t.Fatal("foreground admission not completed", err)
	}
	// A distinct post-stop failure must expose the outage and remain terminal for
	// this UUID even if the missing executable is later restored.
	if err = client.Unlock(ctx, pass, false); err != nil {
		t.Fatal(err)
	}
	copiedExe := filepath.Join(root, "disposable-app.exe")
	source, err := os.Open(exe)
	if err != nil {
		t.Fatal(err)
	}
	target, err := os.OpenFile(copiedExe, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0700)
	if err != nil {
		source.Close()
		t.Fatal(err)
	}
	_, copyErr := io.Copy(target, source)
	source.Close()
	closeErr := target.Close()
	if copyErr != nil || closeErr != nil {
		t.Fatal("fixture executable copy failed")
	}
	copiedArgs, _ := json.Marshal([]string{copiedExe, "__launcher-test-app", output})
	copiedConfig := strings.Replace(config, string(args), string(copiedArgs), 1)
	if err = os.WriteFile(filepath.Join(root, "devtools.toml"), []byte(copiedConfig), 0600); err != nil {
		t.Fatal(err)
	}
	os.Remove(filepath.Join(root, "stop-entered"))
	os.Remove(filepath.Join(root, "release-stop"))
	beforeReturn = func() {}
	failedRequest := services.Request{Action: "restart", ID: next.Item.ID, RequestID: tasks.ID()}
	go func() { r, e := service.Apply(ctx, failedRequest); completed <- outcome{r, e} }()
	deadline = time.Now().Add(3 * time.Second)
	for {
		if _, err := os.Stat(filepath.Join(root, "stop-entered")); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("failure stop boundary not reached")
		}
		time.Sleep(time.Millisecond)
	}
	if err = os.Remove(copiedExe); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(root, "release-stop"), []byte("release"), 0600); err != nil {
		t.Fatal(err)
	}
	select {
	case result = <-completed:
	case <-time.After(5 * time.Second):
		t.Fatal("post-stop spawn failure not observed")
	}
	if result.err != nil || result.result.Item.State != "failed" || !result.result.Item.Outage || (result.result.Item.Reason != "execution_failed" && result.result.Item.Reason != "command_not_found") {
		t.Fatalf("post-stop failure not separated: %v state=%s outage=%v reason=%s", result.err, result.result.Item.State, result.result.Item.Outage, result.result.Item.Reason)
	}
	failedReplay, replayErr := service.Apply(ctx, failedRequest)
	if replayErr != nil || !failedReplay.Replayed || failedReplay.Item.ID != result.result.Item.ID {
		t.Fatal("failed UUID was relaunched", replayErr)
	}
	if err = client.Lock(ctx); err != nil {
		t.Fatal("known spawn failure did not drain", err)
	}

}
