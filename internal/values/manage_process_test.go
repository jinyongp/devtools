package values

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/jinyongp/devtools/internal/vault"
)

type managedMutationFixture struct {
	Root   string
	Phase  string
	Change Change
}

func runManagedMutationFixture() int {
	var request managedMutationFixture
	if json.NewDecoder(io.LimitReader(os.Stdin, 65536)).Decode(&request) != nil {
		return 91
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	s := CurrentStore(filepath.Join(request.Root, "profiles"), "app")
	wait := func() {
		_, _ = io.WriteString(os.Stdout, "checkpoint\n")
		var signal [1]byte
		_, _ = io.ReadFull(os.Stdin, signal[:])
	}
	if request.Phase == "candidate" {
		s.beforeCommit = wait
	}
	result, e := s.Apply(ctx, request.Change)
	if e != nil {
		return 92
	}
	if request.Phase == "response" {
		wait()
	}
	if json.NewEncoder(os.Stdout).Encode(result) != nil {
		return 93
	}
	return 0
}

func TestManagedMutationRealProcessResponseLossAndCandidateKill(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, phase := range []string{"candidate", "response"} {
		t.Run(phase, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			root, files, master, _ := vaultSnapshotFixture(t, 0)
			directory := filepath.Join(root, "vault")
			daemon := exec.CommandContext(ctx, executable, "__vault-session", directory)
			if err := daemon.Start(); err != nil {
				t.Fatal(err)
			}
			defer func() { cancel(); _ = daemon.Process.Kill(); _ = daemon.Wait() }()
			var client *vault.SessionClient
			for {
				client, err = vault.OpenSessionClient(directory)
				if err == nil {
					if _, err = client.Status(ctx); err == nil {
						break
					}
				}
				select {
				case <-ctx.Done():
					t.Fatal("owned daemon not ready")
				case <-time.After(10 * time.Millisecond):
				}
			}
			pass := vault.NewMaterial([]byte("synthetic-snapshot-unlock-only"))
			defer pass.Destroy()
			if err := client.Unlock(ctx, pass, false); err != nil {
				t.Fatal(err)
			}
			store := CurrentStore(filepath.Join(root, "profiles"), "app")
			view, e := store.Inspect(ctx, "")
			if e != nil {
				t.Fatal(e)
			}
			c := Change{Action: "secret.generate", Key: "TOKEN", Revision: view.Revision, RequestID: testRequestID(t), Generate: &GenerateChange{}}
			request := managedMutationFixture{Root: root, Phase: phase, Change: c}
			writer := exec.CommandContext(ctx, executable, "__managed-mutation-fixture")
			stdin, err := writer.StdinPipe()
			if err != nil {
				t.Fatal(err)
			}
			defer stdin.Close()
			stdout, err := writer.StdoutPipe()
			if err != nil {
				t.Fatal(err)
			}
			if err := writer.Start(); err != nil {
				t.Fatal(err)
			}
			waited := false
			defer func() {
				if !waited {
					_ = writer.Process.Kill()
					_ = writer.Wait()
				}
			}()
			if err := json.NewEncoder(stdin).Encode(request); err != nil {
				t.Fatal(err)
			}
			scanner := bufio.NewScanner(stdout)
			if !scanner.Scan() || scanner.Text() != "checkpoint" {
				t.Fatal("actual writer did not reach checkpoint")
			}
			if err := writer.Process.Kill(); err != nil {
				t.Fatal(err)
			}
			if err := writer.Wait(); err == nil {
				t.Fatal("writer did not terminate by kill")
			}
			waited = true
			lookup, e := store.InspectRequest(ctx, c.RequestID)
			if phase == "candidate" {
				if e == nil || e.Code != "request_not_found" {
					t.Fatal("uncommitted candidate bound request", e)
				}
				p, key, err := files.ReadWithMaster(ctx, "app", master)
				if err != nil {
					t.Fatal(err)
				}
				key.Destroy()
				if len(p.Values) != 0 {
					t.Fatal("killed candidate published secret")
				}
			} else if e != nil || lookup.Item.Result == nil {
				t.Fatal("committed response loss lost receipt", e)
			}
			if phase == "response" {
				newer := "synthetic-post-loss-secret"
				view, e := store.Inspect(ctx, "")
				if e != nil {
					t.Fatal(e)
				}
				if _, e := store.Apply(ctx, Change{Action: "secret.set", Key: "TOKEN", Value: &newer, Revision: view.Revision, RequestID: testRequestID(t)}); e != nil {
					t.Fatal(e)
				}
			}
			// A new client process replays only after acquiring a new real session
			// ticket; no in-process cache/result substitute is involved.
			request.Phase = "apply"
			body, _ := json.Marshal(request)
			retry := exec.CommandContext(ctx, executable, "__managed-mutation-fixture")
			retry.Stdin = bytes.NewReader(body)
			out, err := retry.Output()
			if err != nil {
				t.Fatal("fresh retry failed", err)
			}
			var result ChangeResult
			if err := json.Unmarshal(out, &result); err != nil {
				t.Fatal(err)
			}
			if result.Replayed != (phase == "response") {
				t.Fatal("retry confused precommit and committed result")
			}
			if phase == "response" {
				if result.Revision != lookup.Item.Result.Revision || generatedToken(t, files, master, "") != "synthetic-post-loss-secret" {
					t.Fatal("lost response retry rolled back later writer")
				}
			}
			if err := files.VerifyStorage(ctx); err != nil {
				t.Fatal(err)
			}
		})
	}
}
