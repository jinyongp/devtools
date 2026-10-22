package cli

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/jinyongp/devtools/internal/protocol"
)

// This receiver consumes only CLI address metadata and implements the wire
// contract independently. Do not replace it with a vault/valuehandoff helper:
// those helpers prepare unpublished directories and would hide this regression.
type externalRunnerAddress struct {
	RootID            string `json:"root_id"`
	VaultID           string `json:"vault_id"`
	RunnerID          string `json:"runner_id"`
	InstanceID        string `json:"instance_id"`
	Endpoint          string `json:"endpoint"`
	Protocol          int    `json:"protocol"`
	RedactionProtocol int    `json:"redaction_protocol"`
}

type externalRunnerControl struct {
	Protocol            int    `json:"protocol"`
	Type                string `json:"type"`
	RootID              string `json:"root_id,omitempty"`
	VaultID             string `json:"vault_id,omitempty"`
	RunnerID            string `json:"runner_id,omitempty"`
	InstanceID          string `json:"instance_id,omitempty"`
	ServerChallenge     string `json:"server_challenge"`
	ClientChallenge     string `json:"client_challenge,omitempty"`
	ExecutionID         string `json:"execution_id,omitempty"`
	InputID             string `json:"input_id,omitempty"`
	ExternalExecutionID string `json:"external_execution_id,omitempty"`
	DeliveryID          string `json:"delivery_id,omitempty"`
	RedactionProtocol   int    `json:"redaction_protocol,omitempty"`
}

func externalRunnerFrame(c io.ReadWriter, out any, send bool) error {
	var length [4]byte
	if send {
		body, err := json.Marshal(out)
		if err != nil {
			return err
		}
		binary.BigEndian.PutUint32(length[:], uint32(len(body)))
		_, err = io.Copy(c, io.MultiReader(bytes.NewReader(length[:]), bytes.NewReader(body)))
		return err
	}
	if _, err := io.ReadFull(c, length[:]); err != nil {
		return err
	}
	n := binary.BigEndian.Uint32(length[:])
	if n == 0 || n > 128<<10 { // All synthetic fixture frames fit the control limit.
		return fmt.Errorf("invalid fixture frame length")
	}
	body := make([]byte, n)
	defer clear(body)
	if _, err := io.ReadFull(c, body); err != nil {
		return err
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(out); err != nil {
		return err
	}
	if decoder.Decode(new(any)) != io.EOF {
		return fmt.Errorf("trailing fixture frame data")
	}
	return nil
}

type externalRunner struct {
	address     externalRunnerAddress
	connections atomic.Int32
	payloads    atomic.Int32
	dropACK     atomic.Bool
}

func (r *externalRunner) receive(c *net.UnixConn) error {
	defer c.Close()
	if err := c.SetDeadline(time.Now().Add(3 * time.Second)); err != nil {
		return err
	}
	raw, err := c.SyscallConn()
	if err != nil {
		return err
	}
	var credential *syscall.Ucred
	var peerError error
	if err := raw.Control(func(fd uintptr) {
		credential, peerError = syscall.GetsockoptUcred(int(fd), syscall.SOL_SOCKET, syscall.SO_PEERCRED)
	}); err != nil || peerError != nil || credential == nil || credential.Uid != uint32(os.Getuid()) {
		return fmt.Errorf("receiver peer UID mismatch")
	}
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return err
	}
	a := r.address
	hello := externalRunnerControl{Protocol: 1, Type: "hello", RootID: a.RootID, VaultID: a.VaultID, RunnerID: a.RunnerID, InstanceID: a.InstanceID, ServerChallenge: hex.EncodeToString(nonce[:])}
	if err := externalRunnerFrame(c, hello, true); err != nil {
		return err
	}
	var request externalRunnerControl
	if err := externalRunnerFrame(c, &request, false); err != nil {
		return err
	}
	challenge, err := hex.DecodeString(request.ClientChallenge)
	if err != nil || len(challenge) != 16 || request.Protocol != 1 || request.RootID != a.RootID || request.VaultID != a.VaultID || request.RunnerID != a.RunnerID || request.InstanceID != a.InstanceID || request.ServerChallenge != hello.ServerChallenge {
		return fmt.Errorf("receiver binding mismatch")
	}
	response := externalRunnerControl{Protocol: 1, ServerChallenge: hello.ServerChallenge, ClientChallenge: request.ClientChallenge, RedactionProtocol: 1}
	if request.Type == "probe" {
		response.Type = "registered"
		return externalRunnerFrame(c, response, true)
	}
	if request.Type != "bind" || len(request.ExecutionID) != 32 || len(request.InputID) != 32 || request.ExternalExecutionID == "" {
		return fmt.Errorf("receiver expected bound delivery")
	}
	response.Type = "ready"
	if err := externalRunnerFrame(c, response, true); err != nil {
		return err
	}
	var payload struct {
		Protocol            int    `json:"protocol"`
		Type                string `json:"type"`
		ExecutionID         string `json:"execution_id"`
		InputID             string `json:"input_id"`
		DeliveryID          string `json:"delivery_id"`
		ExternalExecutionID string `json:"external_execution_id"`
		Values              []struct {
			Key   string `json:"key"`
			Kind  string `json:"kind"`
			Value string `json:"value"`
		} `json:"values"`
		Redaction struct {
			Protocol int      `json:"protocol"`
			Patterns [][]byte `json:"patterns"`
		} `json:"redaction"`
	}
	if err := externalRunnerFrame(c, &payload, false); err != nil {
		return err
	}
	defer func() {
		clear(payload.Values)
		for _, pattern := range payload.Redaction.Patterns {
			clear(pattern)
		}
	}()
	if payload.Protocol != 1 || payload.Type != "payload" || payload.ExecutionID != request.ExecutionID || payload.InputID != request.InputID || payload.ExternalExecutionID != request.ExternalExecutionID || len(payload.DeliveryID) != 32 || len(payload.Values) != 1 || payload.Values[0].Key != "TOKEN" || payload.Values[0].Kind != "secret" || payload.Values[0].Value != externalRunnerCanary || payload.Redaction.Protocol != 1 || len(payload.Redaction.Patterns) != 1 || !bytes.Equal(payload.Redaction.Patterns[0], []byte(externalRunnerCanary)) {
		return fmt.Errorf("receiver payload mismatch")
	}
	// No application output is produced. The existing receiver lifetime tests
	// exercise streaming masking after lock; this fixture isolates transport.
	r.payloads.Add(1)
	if r.dropACK.Load() {
		return nil
	}
	response.Type, response.ExecutionID, response.InputID, response.DeliveryID = "accepted", payload.ExecutionID, payload.InputID, payload.DeliveryID
	return externalRunnerFrame(c, response, true)
}

const externalRunnerCanary = "synthetic-external-receiver-only-secret"

func TestExternalRunnerUsesOnlyPublicEndpointMetadata(t *testing.T) {
	for _, suffix := range []string{"short", strings.Repeat("long", 35)} {
		name := "short"
		if len(suffix) > 5 {
			name = "long"
		}
		t.Run(name, func(t *testing.T) {
			app := testApp(t)
			base, _ := app.dataDirectory()
			root := filepath.Join(filepath.Dir(base), suffix)
			app.dataDirectory = func() (string, *protocol.Error) { return filepath.Join(root, "profiles"), nil }
			initializeTestVault(t, app)
			call := func(wantError, input string, args ...string) json.RawMessage {
				t.Helper()
				code, stdout, stderr := invoke(t, app, input, args...)
				for _, private := range []string{externalRunnerCanary, base64.StdEncoding.EncodeToString([]byte(externalRunnerCanary)), `"patterns"`} {
					if strings.Contains(stdout+stderr, private) {
						t.Fatal("CLI exposed private payload")
					}
				}
				var envelope struct {
					Data  json.RawMessage       `json:"data"`
					Error struct{ Code string } `json:"error"`
				}
				body := stdout
				if code != 0 {
					body = stderr
				}
				if json.Unmarshal([]byte(body), &envelope) != nil || envelope.Error.Code != wantError || (wantError == "" && code != 0) || (wantError != "" && code != 3) {
					t.Fatalf("%v: unexpected CLI result (%d, %s)", args, code, envelope.Error.Code)
				}
				return envelope.Data
			}
			call("", externalRunnerCanary, "secret", "set", "TOKEN", "--profile", "app", "--stdin")
			const runner, instance = "01234567-89ab-4cde-8fab-0123456789ab", "01234567-89ab-4cde-8fab-0123456789ac"
			metadata := call("", "", "values", "runner", "address", runner, "--instance", instance)
			var receiver externalRunner
			if json.Unmarshal(metadata, &receiver.address) != nil || receiver.address.Protocol != 1 || receiver.address.RedactionProtocol != 1 {
				t.Fatal("invalid public address metadata")
			}
			assertNoInternalTransport := func() {
				t.Helper()
				if _, err := os.Lstat(filepath.Join(root, "value-runner-transports")); !os.IsNotExist(err) {
					t.Fatal("consumer unexpectedly needed an internal transport tree", err)
				}
			}
			assertNoInternalTransport()
			endpoint := receiver.address.Endpoint
			if !strings.HasPrefix(endpoint, "/tmp/") || len(endpoint) >= 100 {
				// Linux runner UUID paths always exceed the in-place socket limit.
				t.Fatal("expected short fallback endpoint")
			}
			if err := os.Mkdir(filepath.Dir(endpoint), 0700); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { os.Remove(filepath.Dir(endpoint)) })
			listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: endpoint, Net: "unix"})
			if err != nil {
				t.Fatal(err)
			}
			if err := os.Chmod(endpoint, 0600); err != nil {
				listener.Close()
				t.Fatal(err)
			}
			done := make(chan error, 1)
			go func() {
				for {
					c, err := listener.AcceptUnix()
					if err != nil {
						done <- nil
						return
					}
					receiver.connections.Add(1)
					if err := receiver.receive(c); err != nil {
						done <- err
						return
					}
				}
			}()
			t.Cleanup(func() {
				listener.Close()
				if err := <-done; err != nil {
					t.Error("independent receiver failed", err)
				}
			})
			uuid := func(n int) string { return fmt.Sprintf("00000000-0000-4000-8000-%012d", n) }
			registration := fmt.Sprintf(`{"runner_id":%q,"instance_id":%q,"protocol":1,"redaction_protocol":1}`, runner, instance)
			registerArgs := []string{"values", "runner", "register", "--stdin", "--request-id", uuid(1)}
			call("", registration, registerArgs...)
			if replay := call("", registration, registerArgs...); !bytes.Contains(replay, []byte(`"replayed":true`)) {
				t.Fatal("registration receipt did not replay")
			}
			if receiver.connections.Load() != 1 {
				t.Fatal("registration replay probed the receiver")
			}
			for i := 0; i < 2; i++ {
				receiver.dropACK.Store(i == 1)
				job := fmt.Sprintf("external-job-%d", i)
				prepared := call("", fmt.Sprintf(`{"external_execution_id":%q,"selection":{"keys":["TOKEN"]}}`, job), "values", "execution", "prepare", "--profile", "app", "--stdin", "--request-id", uuid(10+i*10))
				var result struct {
					Item struct {
						ExecutionID string `json:"execution_id"`
						InputID     string `json:"input_id"`
						Revision    int    `json:"revision"`
					} `json:"item"`
				}
				if err := json.Unmarshal(prepared, &result); err != nil {
					t.Fatal(err)
				}
				approval := fmt.Sprintf(`{"revision":%d,"runner_id":%q,"runner_instance_id":%q,"external_execution_id":%q,"input_id":%q}`, result.Item.Revision, runner, instance, job, result.Item.InputID)
				approved := call("", approval, "values", "execution", "approve", result.Item.ExecutionID, "--profile", "app", "--stdin", "--request-id", uuid(11+i*10))
				if err := json.Unmarshal(approved, &result); err != nil {
					t.Fatal(err)
				}
				body := fmt.Sprintf(`{"revision":%d}`, result.Item.Revision)
				args := []string{"values", "execution", "deliver", result.Item.ExecutionID, "--profile", "app", "--stdin", "--request-id", uuid(12 + i*10)}
				wantError := ""
				if i == 1 {
					wantError = "delivery_outcome_unknown"
				}
				call(wantError, body, args...)
				connections := receiver.connections.Load()
				call(wantError, body, args...)
				args[len(args)-1] = uuid(13 + i*10)
				if i == 0 {
					wantError = "delivery_already_started"
				}
				call(wantError, body, args...)
				if receiver.payloads.Load() != int32(i+1) || receiver.connections.Load() != connections {
					t.Fatal("retry reconnected or duplicated a private payload")
				}
			}
			call("", "", "vault", "lock")
			if locked := call("", "", "values", "runner", "address", runner, "--instance", instance); !bytes.Equal(locked, metadata) {
				t.Fatal("passive address changed after lock")
			}
			assertNoInternalTransport()
		})
	}
}
