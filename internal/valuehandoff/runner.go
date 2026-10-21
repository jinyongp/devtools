package valuehandoff

import (
	"context"
	"crypto/hmac"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"time"

	"github.com/jinyongp/devtools/internal/maintenance"
	"github.com/jinyongp/devtools/internal/protocol"
	"github.com/jinyongp/devtools/internal/values"
	"github.com/jinyongp/devtools/internal/vault"
)

type RunnerInput struct {
	RunnerID          string `json:"runner_id"`
	InstanceID        string `json:"instance_id"`
	Protocol          int    `json:"protocol"`
	RedactionProtocol int    `json:"redaction_protocol"`
}
type Runner struct {
	RunnerID          string `json:"runner_id"`
	InstanceID        string `json:"instance_id"`
	Protocol          int    `json:"protocol"`
	RedactionProtocol int    `json:"redaction_protocol"`
	Enabled           bool   `json:"enabled"`
}
type RunnerResult struct {
	Profile  string `json:"profile"`
	Item     Runner `json:"item"`
	Changed  bool   `json:"changed"`
	Replayed bool   `json:"replayed"`
}
type runnerReceipt struct {
	Fingerprint string       `json:"fingerprint"`
	CreatedAt   time.Time    `json:"created_at"`
	ExpiresAt   time.Time    `json:"expires_at"`
	Result      RunnerResult `json:"result"`
}
type runnerReceiptOuter struct {
	ScopeID  string          `json:"scope_id"`
	Document json.RawMessage `json:"document"`
}

func (s Store) registry() Store { return Store{Root: s.Root, Profile: "runners"} }
func (s Store) readRunner(ctx context.Context, a *access, id string) (Runner, error) {
	var r Runner
	err := s.registry().decode(ctx, a, s.path("value-runners", id), id, vault.MaterialRunnerRegistration, &r)
	if err == nil && (r.RunnerID != id || !uuidRE.MatchString(r.InstanceID) || r.Protocol != 1 || r.RedactionProtocol != 1) {
		err = vault.ErrIntegrity
	}
	return r, err
}
func (s Store) replayRunner(ctx context.Context, a *access, id, fp string) (RunnerResult, bool, *protocol.Error) {
	path := s.path("value-runner-requests", id)
	b, err := maintenance.Read(filepath.Join(s.Root, path), vault.MaxDocumentBytes)
	if errors.Is(err, os.ErrNotExist) {
		return RunnerResult{}, false, nil
	}
	if err != nil {
		return RunnerResult{}, false, values.VaultError(ctx, err)
	}
	defer clear(b)
	var outer runnerReceiptOuter
	if vault.StrictDecode(b, &outer) != nil || !idRE.MatchString(outer.ScopeID) {
		return RunnerResult{}, true, failure(vault.ErrIntegrity.Error())
	}
	m, err := vault.OpenProtected(ctx, a.master, a.vaultID, outer.ScopeID, vault.MaterialExecutionRequest, outer.Document)
	if err != nil {
		return RunnerResult{}, true, values.VaultError(ctx, err)
	}
	defer m.Destroy()
	plain := m.CopyBytes()
	defer clear(plain)
	var binding bound
	if vault.StrictDecode(plain, &binding) != nil || binding.Version != 1 || binding.Root != filepath.Clean(s.Root) || binding.Path != path || binding.Profile != "runners" {
		return RunnerResult{}, true, failure(vault.ErrIntegrity.Error())
	}
	var r runnerReceipt
	if vault.StrictDecode(binding.Data, &r) != nil {
		return RunnerResult{}, true, failure(vault.ErrIntegrity.Error())
	}
	if !time.Now().Before(r.ExpiresAt) {
		return RunnerResult{}, true, failure("request_expired")
	}
	if !hmac.Equal([]byte(r.Fingerprint), []byte(fp)) {
		return RunnerResult{}, true, failure("request_conflict")
	}
	r.Result.Replayed = true
	return r.Result, true, nil
}
func (s Store) runnerMutation(ctx context.Context, input RunnerInput, requestID string, disable bool) (RunnerResult, *protocol.Error) {
	if !uuidRE.MatchString(input.RunnerID) || !uuidRE.MatchString(input.InstanceID) || !uuidRE.MatchString(requestID) || input.Protocol != 1 || input.RedactionProtocol != 1 {
		return RunnerResult{}, invalid()
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	reg := s.registry()
	a, err := reg.access(ctx, false)
	if err != nil {
		return RunnerResult{}, values.VaultError(ctx, err)
	}
	defer a.master.Destroy()
	action := "runner.register"
	if disable {
		action = "runner.unregister"
	}
	fp, err := fingerprint(a, action, input.RunnerID, input)
	if err != nil {
		return RunnerResult{}, invalid()
	}
	read, err := maintenance.AcquireShared(ctx, s.Root)
	if err != nil {
		return RunnerResult{}, values.VaultError(ctx, err)
	}
	saved, found, e := s.replayRunner(ctx, a, requestID, fp)
	read()
	if found || e != nil {
		return saved, e
	}
	if !disable {
		address, err := Address(s.Root, input.RunnerID, input.InstanceID)
		if err != nil {
			return RunnerResult{}, values.VaultError(ctx, err)
		}
		if err = probeRunner(ctx, s.Root, address); err != nil {
			if ctx.Err() != nil {
				return RunnerResult{}, values.VaultError(ctx, ctx.Err())
			}
			return RunnerResult{}, failure("runner_protocol_mismatch")
		}
	}
	// Disable and new delivery intent serialize through the physical launch
	// barrier, without waiting for per-execution locks or holding an IPC mutex.
	launch, err := maintenance.LockExclusive(ctx, filepath.Join(s.Root, "vault", "launch.lock"))
	if err != nil {
		return RunnerResult{}, values.VaultError(ctx, err)
	}
	defer launch()
	commit, err := reg.lockCommit(ctx, a)
	if err != nil {
		return RunnerResult{}, values.VaultError(ctx, err)
	}
	defer commit()
	if saved, found, e := s.replayRunner(ctx, a, requestID, fp); found || e != nil {
		return saved, e
	}
	current, err := s.readRunner(ctx, a, input.RunnerID)
	missing := errors.Is(err, os.ErrNotExist)
	if err != nil && !missing {
		return RunnerResult{}, values.VaultError(ctx, err)
	}
	if disable && missing {
		return RunnerResult{}, failure("runner_not_found")
	}
	if !missing && ((disable && current.InstanceID != input.InstanceID) || (!disable && current.Enabled && current.InstanceID != input.InstanceID)) {
		return RunnerResult{}, failure("runner_instance_conflict")
	}
	next := Runner{input.RunnerID, input.InstanceID, 1, 1, !disable}
	changed := missing || next != current
	result := RunnerResult{Profile: "", Item: next, Changed: changed}
	path := s.path("value-runners", input.RunnerID)
	b, err := reg.encode(ctx, a, path, input.RunnerID, vault.MaterialRunnerRegistration, next)
	if err != nil {
		return RunnerResult{}, values.VaultError(ctx, err)
	}
	now := time.Now().UTC()
	rr := runnerReceipt{fp, now, now.Add(30 * 24 * time.Hour), result}
	id, err := vault.NewID()
	if err != nil {
		return RunnerResult{}, values.VaultError(ctx, err)
	}
	rp := s.path("value-runner-requests", requestID)
	document, err := reg.encode(ctx, a, rp, id, vault.MaterialExecutionRequest, rr)
	if err != nil {
		return RunnerResult{}, values.VaultError(ctx, err)
	}
	outer, err := json.Marshal(runnerReceiptOuter{id, document})
	if err != nil {
		return RunnerResult{}, values.VaultError(ctx, err)
	}
	if err = maintenance.Replace(s.Root, map[string][]byte{path: b, rp: outer}); err != nil {
		return RunnerResult{}, values.VaultError(ctx, err)
	}
	return result, nil
}
func (s Store) Register(ctx context.Context, input RunnerInput, requestID string) (RunnerResult, *protocol.Error) {
	return s.runnerMutation(ctx, input, requestID, false)
}
func (s Store) Unregister(ctx context.Context, id, instance, requestID string) (RunnerResult, *protocol.Error) {
	return s.runnerMutation(ctx, RunnerInput{id, instance, 1, 1}, requestID, true)
}
func (s Store) Runner(ctx context.Context, id string) (Runner, *protocol.Error) {
	if !uuidRE.MatchString(id) {
		return Runner{}, invalid()
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	a, err := s.registry().access(ctx, false)
	if err != nil {
		return Runner{}, values.VaultError(ctx, err)
	}
	defer a.master.Destroy()
	release, err := maintenance.AcquireShared(ctx, s.Root)
	if err != nil {
		return Runner{}, values.VaultError(ctx, err)
	}
	defer release()
	r, err := s.readRunner(ctx, a, id)
	if errors.Is(err, os.ErrNotExist) {
		return Runner{}, failure("runner_not_found")
	}
	if err != nil {
		return Runner{}, values.VaultError(ctx, err)
	}
	return r, nil
}
