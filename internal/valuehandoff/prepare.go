package valuehandoff

import (
	"context"
	"path/filepath"
	"time"

	"github.com/jinyongp/devtools/internal/maintenance"
	"github.com/jinyongp/devtools/internal/protocol"
	"github.com/jinyongp/devtools/internal/values"
	"github.com/jinyongp/devtools/internal/vault"
)

func (s Store) Prepare(ctx context.Context, input PrepareInput, requestID string) (Result, *protocol.Error) {
	started := time.Now().UTC()
	if !uuidRE.MatchString(requestID) || !opaque(input.ExternalExecutionID) || !input.Selection.valid() || input.Env != "" && !vault.ValidSelection(input.Env) {
		return Result{}, invalid()
	}
	a, err := s.access(ctx, true)
	if err != nil {
		return Result{}, values.VaultError(ctx, err)
	}
	defer a.master.Destroy()
	fp, err := fingerprint(a, "prepare", "", input)
	if err != nil {
		return Result{}, invalid()
	}
	read, err := maintenance.AcquireShared(ctx, s.Root)
	if err != nil {
		return Result{}, values.VaultError(ctx, err)
	}
	result, found, e := s.replay(ctx, a, requestID, fp)
	if found || e != nil {
		read()
		return result, e
	}
	p, key, err := (vault.FileStore{Root: s.Root, VaultID: a.vaultID, CatalogKey: a.master}).ReadAnyWithMasterHeld(ctx, s.Profile, a.master)
	if err != nil {
		read()
		return Result{}, values.VaultError(ctx, err)
	}
	effective, bundleID, err := p.ResolveScopedValues(input.Env, key, started, input.Selection.Keys, input.Selection.Bundle)
	key.Destroy()
	read()
	if err != nil {
		if err.Error() == "selection_outside_scope" {
			return Result{}, failure("selection_outside_scope")
		}
		return Result{}, values.VaultError(ctx, err)
	}
	defer func() {
		for i := range effective {
			effective[i].Value = ""
		}
	}()
	id, err := vault.NewID()
	if err != nil {
		return Result{}, values.VaultError(ctx, err)
	}
	inputID, err := vault.NewID()
	if err != nil {
		return Result{}, values.VaultError(ctx, err)
	}
	deadline := started.Add(30 * time.Second)
	if a.ticket.ExpiresAt.Before(deadline) {
		deadline = a.ticket.ExpiresAt
	}
	snap := snapshot{Values: []storedValue{}}
	defer func() { clear(snap.Values) }()
	slots := []vault.SlotReference{}
	for _, v := range effective {
		snap.Values = append(snap.Values, storedValue{v.Reference.Name, v.Kind, v.Value})
		slots = append(slots, v.Reference)
		if v.ExpiresAt != nil {
			t := time.Unix(*v.ExpiresAt, 0).UTC()
			if t.Before(deadline) {
				deadline = t
			}
		}
	}
	ticket := *a.ticket
	ticket.Profile = s.Profile
	ticket.ProfileID = p.ID
	ticket.StorageGeneration = p.Generation
	ticket.KeyEpoch = p.KeyEpoch
	r := record{Item: Execution{ID: id, InputID: inputID, ExternalExecutionID: input.ExternalExecutionID, Revision: 1, CreatedAt: started, ExpiresAt: deadline, DeliveryState: "prepared", ExecutionState: "not_started"}, Ticket: ticket, Protection: a.identity, Env: input.Env, BundleID: bundleID, Slots: slots}
	ctx, cancel := context.WithDeadline(ctx, deadline)
	defer cancel()
	op, err := s.operationLease(ctx, id)
	if err != nil {
		return Result{}, values.VaultError(ctx, err)
	}
	defer op()
	launch, err := a.client.BeginDelivery(ctx, ticket)
	if err != nil {
		return Result{}, values.VaultError(ctx, err)
	}
	defer launch()
	commit, err := s.lockCommit(ctx, a)
	if err != nil {
		return Result{}, values.VaultError(ctx, err)
	}
	defer commit()
	if result, found, e := s.replay(ctx, a, requestID, fp); found || e != nil {
		return result, e
	}
	// Generation is fenced by the launch lease; ordinary value edits do not
	// reselect or alter these already authenticated bytes.
	if !time.Now().Before(deadline) {
		return Result{}, failure("execution_input_expired")
	}
	result = Result{Profile: s.Profile, Item: r.Item, Changed: true}
	files, err := s.recordFiles(ctx, a, r, requestID, fp, "prepare", result, false)
	if err == nil {
		type encodedSnapshot snapshot
		path := s.path("value-execution-payloads", id)
		files[path], err = s.encode(ctx, a, path, id, vault.MaterialExecutionSnapshot, encodedSnapshot(snap))
	}
	if err == nil {
		err = maintenance.Replace(s.Root, files)
	}
	if err != nil {
		return Result{}, values.VaultError(ctx, err)
	}
	return result, nil
}

// ValidateFrozenHeld checks generation under an existing maintenance gate.
// Ordinary edits are permitted; replacement/rotation invalidates admission.
func (s Store) validateFrozenHeld(ctx context.Context, a *access, r record) error {
	p, key, err := (vault.FileStore{Root: s.Root, VaultID: a.vaultID, CatalogKey: a.master}).ReadAnyWithMasterHeld(ctx, s.Profile, a.master)
	if err != nil {
		return err
	}
	defer key.Destroy()
	if p.ID != r.Ticket.ProfileID || p.Generation != r.Ticket.StorageGeneration || p.KeyEpoch != r.Ticket.KeyEpoch {
		return vault.ErrInputChanged
	}
	identity, err := (vault.KeyFileStore{Directory: filepath.Join(s.Root, "vault")}).WriteIdentity()
	if err != nil {
		return err
	}
	if identity != r.Protection {
		return vault.ErrInputChanged
	}
	return nil
}
