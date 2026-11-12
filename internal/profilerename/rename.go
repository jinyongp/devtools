package profilerename

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/jinyongp/devtools/internal/expose"
	"github.com/jinyongp/devtools/internal/location"
	"github.com/jinyongp/devtools/internal/maintenance"
	"github.com/jinyongp/devtools/internal/ports"
	"github.com/jinyongp/devtools/internal/profilekey"
	"github.com/jinyongp/devtools/internal/project"
	"github.com/jinyongp/devtools/internal/protocol"
	"github.com/jinyongp/devtools/internal/services"
	"github.com/jinyongp/devtools/internal/tasks"
	"github.com/jinyongp/devtools/internal/valuehandoff"
	"github.com/jinyongp/devtools/internal/values"
	"github.com/jinyongp/devtools/internal/vault"
)

const renameTaskSnapshotLimit = 128 << 20

var renameRequestID = regexp.MustCompile(valuehandoff.UUIDPattern)

type Plan struct {
	Source string `json:"source_profile"`
	Target string `json:"target_profile"`
	Digest string `json:"digest"`
	Values bool   `json:"values"`
	Tasks  bool   `json:"tasks"`
	Ports  bool   `json:"ports"`
}

type renameCandidate struct {
	plan      Plan
	names     profilekey.RenameNames
	portFiles map[string][]byte
}

type renameReceipt struct {
	Version   int    `json:"version"`
	RequestID string `json:"request_id"`
	Result    Plan   `json:"result"`
}

type transitionRequest struct {
	Source, Target, Digest, RequestID string
}

type Store struct {
	Data string
}

func renameError(code, message string, details map[string]any) *protocol.Error {
	return protocol.NewError(code, message, 3, details)
}

func renameStorageError(ctx context.Context, operation string, err error) *protocol.Error {
	if condition := protocol.ContextError(ctx.Err()); condition != nil {
		return condition
	}
	var condition *protocol.Error
	if errors.As(err, &condition) {
		return condition
	}
	if errors.Is(err, maintenance.ErrRuntimeWriterActive) {
		if operation == "publish_runtime_transition" {
			return renameError("runtime_writer_active", "An already-admitted writer still owns the previous storage. Wait for it to exit, then retry the same name change.", nil)
		}
		return renameError("runtime_writer_active", "Stop active devtools processes, proxies, and exposures before retrying the same name change.", nil)
	}
	if errors.Is(err, maintenance.ErrLegacyRuntimeRecreated) {
		return renameError("storage_transition_conflict", "An older writer recreated a storage directory while it was being moved. Both copies were preserved for recovery.", nil)
	}
	if errors.Is(err, maintenance.ErrRuntimeTransitionPending) {
		return renameError("storage_transition_pending", "Retry the original name change with its preview digest and request ID to resume the storage transition.", nil)
	}
	failure := protocol.WithStorageDetails(protocol.NewError("storage_error", "Cannot prepare or publish the profile name change.", 1, nil), "profiles", operation, "profile_store", err)
	var preparation *maintenance.QueryPreparationError
	if errors.As(err, &preparation) {
		return protocol.WithStoragePreparation(failure, preparation.State, preparation.Passive)
	}
	return failure
}

func (c Store) canonical(source, target string) (Store, *protocol.Error) {
	if source == target || !project.ValidProfile(source) || !project.ValidProfile(target) || !filepath.IsAbs(c.Data) {
		return Store{}, protocol.NewError("invalid_argument", "Provide two different valid profile identifiers in a local store.", 2, nil)
	}
	root, err := location.Canonical(c.Data)
	if err != nil {
		return Store{}, renameStorageError(context.Background(), "resolve_store", err)
	}
	return Store{Data: root}, nil
}

func (c Store) renameTaskStore(profile string) tasks.Store {
	return tasks.Store{Directory: filepath.Join(c.Data, "tasks"), Profile: profile}
}

// Preview inspects an existing store without preparing locks or recovering it.
func (c Store) Preview(ctx context.Context, source, target string) (Plan, *protocol.Error) {
	c, condition := c.canonical(source, target)
	if condition != nil {
		return Plan{}, condition
	}
	if _, err := os.Lstat(c.Data); errors.Is(err, os.ErrNotExist) {
		return Plan{}, renameError("profile_not_found", "The source profile does not exist in this store.", nil)
	}
	release, err := maintenance.AcquirePassiveQuery(ctx, c.Data)
	if err != nil {
		return Plan{}, renameStorageError(ctx, "preview_rename", err)
	}
	defer release()
	candidate, condition := c.renameCandidateHeld(ctx, source, target)
	return candidate.plan, condition
}

func (c Store) renameCandidateHeld(ctx context.Context, source, target string) (renameCandidate, *protocol.Error) {
	var candidate renameCandidate
	names, err := profilekey.ReadRenameNamesHeld(c.Data)
	if err != nil {
		return candidate, renameStorageError(ctx, "read_rename_names", err)
	}
	owner := names.Owners[source]
	if condition := names.RequireCurrent(source); condition != nil {
		return candidate, condition
	}
	if targetOwner := names.Owners[target]; targetOwner != "" && targetOwner != owner {
		message := fmt.Sprintf("Profile name %q is reserved for profile %q.", target, names.Current[targetOwner])
		return candidate, renameError("profile_name_reserved", message, map[string]any{"profile": target})
	}
	exists := renameError("profile_exists", "The target already has stored profile data.", map[string]any{"profile": target})
	valuesDirectory := filepath.Join(c.Data, "profiles")
	targetValues, err := profilekey.Snapshot(valuesDirectory, target, "values", vault.MaxDocumentBytes)
	if err != nil {
		return candidate, values.QueryError(ctx, err)
	}
	defer clear(targetValues)
	if targetValues != nil {
		return candidate, exists
	}
	targetTasks, condition := c.renameTaskStore(target).ProfileRenameVersionHeld(ctx)
	if condition != nil {
		return candidate, condition
	}
	if targetTasks != nil {
		return candidate, exists
	}
	sourceValues, err := profilekey.Snapshot(valuesDirectory, source, "values", vault.MaxDocumentBytes)
	if err != nil {
		return candidate, values.QueryError(ctx, err)
	}
	defer clear(sourceValues)
	if sourceValues != nil {
		if _, err := vault.DecodeProfile(sourceValues); err != nil {
			return candidate, values.VaultError(ctx, err)
		}
	}
	sourceTasks, condition := c.renameTaskStore(source).ProfileRenameVersionHeld(ctx)
	if condition != nil {
		return candidate, condition
	}
	portFiles, condition := ports.ForDataRoot(c.Data).ProfileRenameFilesHeld(ctx, source, target)
	if condition != nil {
		return candidate, condition
	}
	processes := services.Store{Data: c.Data}
	records, condition := processes.ProfileRenameRecordsHeld(ctx, source, target)
	if condition != nil {
		return candidate, condition
	}
	for _, record := range records {
		if record.Profile == target && names.Owners[target] == "" {
			return candidate, exists
		}
	}
	archives, condition := processes.ArchivedExecutions("")
	if condition != nil {
		return candidate, condition
	}
	relatedArchives := []services.ArchivedExecution{}
	for _, archive := range archives {
		if archive.Owner.Profile == target && names.Owners[target] == "" {
			return candidate, exists
		}
		if archive.Owner.Profile == source || archive.Owner.Profile == target {
			relatedArchives = append(relatedArchives, archive)
		}
	}
	if sourceValues == nil && sourceTasks == nil && len(portFiles) == 0 && len(records) == 0 && len(relatedArchives) == 0 && owner == "" {
		return candidate, renameError("profile_not_found", "The source profile does not exist in this store.", map[string]any{"profile": source})
	}
	vaultID, _, err := maintenance.ProtectedRootHeld(c.Data)
	if err != nil {
		return candidate, renameStorageError(ctx, "read_vault_root", err)
	}
	projection, err := json.Marshal(struct {
		Root, Source, Target, VaultID, Owner, TargetOwner string
		Values, Ports                                     [sha256.Size]byte
		Tasks                                             *tasks.ProfileRenameVersion
		Records                                           []services.Record
		Archives                                          []services.ArchivedExecution
	}{
		c.Data, source, target, vaultID, owner, names.Owners[target],
		sha256.Sum256(sourceValues), sha256.Sum256(portFiles[ports.RegistryRelative(c.Data)]), sourceTasks, records, relatedArchives,
	})
	if err != nil {
		return candidate, renameStorageError(ctx, "encode_rename_plan", err)
	}
	digest := sha256.Sum256(projection)
	candidate.plan = Plan{Source: source, Target: target, Digest: hex.EncodeToString(digest[:]), Values: sourceValues != nil, Tasks: sourceTasks != nil, Ports: len(portFiles) != 0}
	candidate.names = names
	candidate.portFiles = portFiles
	return candidate, nil
}

// Apply closes legacy writer admission once per data root, revalidates the
// preview, and publishes the rename and its request receipt together.
func (c Store) Apply(ctx context.Context, source, target, digest, requestID string, store vault.FileStore) (_ Plan, _ bool, failure *protocol.Error) {
	c, condition := c.canonical(source, target)
	if condition != nil {
		return Plan{}, false, condition
	}
	decoded, decodeErr := hex.DecodeString(digest)
	requestID = strings.ToLower(requestID)
	if decodeErr != nil || len(decoded) != sha256.Size || digest != strings.ToLower(digest) || !renameRequestID.MatchString(requestID) {
		return Plan{}, false, protocol.NewError("invalid_argument", "Provide a preview digest and a request UUID.", 2, nil)
	}
	releaseExecution, err := maintenance.AcquireExclusiveExecution(ctx, c.Data)
	if err != nil {
		return Plan{}, false, renameStorageError(ctx, "exclude_execution", err)
	}
	defer releaseExecution()
	releaseLaunch, err := vault.AcquireLaunchMutation(ctx, filepath.Join(c.Data, "vault"))
	if err != nil {
		return Plan{}, false, values.VaultError(ctx, err)
	}
	defer releaseLaunch()
	release, err := maintenance.AcquireRuntimeTransition(ctx, c.Data)
	if err != nil {
		return Plan{}, false, renameStorageError(ctx, "exclude_storage", err)
	}
	defer release()
	receiptPath := "profile-renames/" + requestID + ".json"
	if raw, err := maintenance.Read(filepath.Join(c.Data, receiptPath), 4096); err == nil {
		var receipt renameReceipt
		if vault.StrictDecode(raw, &receipt) != nil || receipt.Version != 1 || receipt.RequestID != requestID {
			return Plan{}, false, renameStorageError(ctx, "read_rename_receipt", vault.ErrIntegrity)
		}
		if receipt.Result.Source != source || receipt.Result.Target != target || receipt.Result.Digest != digest {
			return Plan{}, false, renameError("request_conflict", "This request ID already belongs to a different profile name change.", nil)
		}
		names, err := profilekey.ReadRenameNamesHeld(c.Data)
		if err != nil {
			return Plan{}, false, renameStorageError(ctx, "read_rename_names", err)
		}
		if owner := names.Owners[source]; owner == "" || names.Owners[target] != owner {
			return Plan{}, false, renameStorageError(ctx, "verify_rename_receipt", vault.ErrIntegrity)
		}
		return receipt.Result, true, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return Plan{}, false, renameStorageError(ctx, "read_rename_receipt", err)
	}
	vaultID, protected, err := maintenance.ProtectedRootHeld(c.Data)
	if err != nil {
		return Plan{}, false, renameStorageError(ctx, "read_vault_root", err)
	}
	if protected && store.CatalogKey == nil {
		return Plan{}, false, values.VaultError(ctx, vault.ErrLocked)
	}
	if store.CatalogKey != nil && (!protected || vaultID != store.VaultID) {
		return Plan{}, false, values.VaultError(ctx, vault.ErrIntegrity)
	}
	store.Root = c.Data
	if protected {
		if err := store.VerifyCatalogHeld(); err != nil {
			return Plan{}, false, values.VaultError(ctx, err)
		}
	}
	for _, name := range []string{source, target} {
		if condition := (valuehandoff.Store{Root: c.Data, Profile: name}).ProfileRenameReadyHeld(ctx, vaultID, store.CatalogKey); condition != nil {
			return Plan{}, false, condition
		}
	}
	request := transitionRequest{Source: source, Target: target, Digest: digest, RequestID: requestID}
	upgraded, condition := c.prepareRuntimeHeld(ctx, request)
	if condition != nil {
		return Plan{}, false, condition
	}
	if upgraded {
		defer func() { failure = protocol.WithEffects(failure, "partial") }()
	}
	candidate, condition := c.renameCandidateHeld(ctx, source, target)
	if condition != nil {
		return Plan{}, false, condition
	}
	plan := candidate.plan
	if plan.Digest != digest {
		return Plan{}, false, renameError("input_changed", "The profile changed. Preview the name change again.", nil)
	}
	valueFiles, err := store.ProfileRenameFilesHeld(ctx, source, target)
	if err != nil {
		return Plan{}, false, values.VaultError(ctx, err)
	}
	taskFiles, err := c.renameTaskStore(source).ProfileRenameFilesHeld(target, renameTaskSnapshotLimit)
	if err != nil {
		return Plan{}, false, renameStorageError(ctx, "stage_tasks", err)
	}
	names := candidate.names
	owner := names.Owners[source]
	if owner == "" {
		owner, err = vault.NewID()
		if err != nil {
			return Plan{}, false, renameStorageError(ctx, "prepare_rename_names", err)
		}
	}
	names.Owners[source], names.Owners[target], names.Current[owner] = owner, owner, target
	namesBody, err := names.Encode()
	if err != nil {
		return Plan{}, false, renameStorageError(ctx, "encode_rename_names", err)
	}
	receipt, err := json.Marshal(renameReceipt{Version: 1, RequestID: requestID, Result: plan})
	if err != nil {
		return Plan{}, false, renameStorageError(ctx, "encode_rename_receipt", err)
	}
	files := map[string][]byte{profilekey.RenameNamesPath: namesBody, receiptPath: receipt}
	for _, domain := range []map[string][]byte{valueFiles, taskFiles, candidate.portFiles} {
		for path, data := range domain {
			if _, present := files[path]; present {
				return Plan{}, false, renameStorageError(ctx, "stage_rename", vault.ErrIntegrity)
			}
			files[path] = data
		}
	}
	if err := ctx.Err(); err != nil {
		return Plan{}, false, protocol.ContextError(err)
	}
	if err := maintenance.Replace(c.Data, files); err != nil {
		return Plan{}, false, protocol.WithEffects(renameStorageError(ctx, "publish_rename", err), "unknown")
	}
	return plan, false, nil
}

func (c Store) prepareRuntimeHeld(ctx context.Context, request transitionRequest) (bool, *protocol.Error) {
	ready, pending, err := maintenance.RuntimeTransitionState(c.Data)
	if err != nil {
		return false, renameStorageError(ctx, "inspect_runtime_transition", err)
	}
	if ready {
		return false, nil
	}
	intentPath := filepath.Join(c.Data, maintenance.VersionedRuntime, "rename-request.json")
	if pending {
		raw, err := maintenance.Read(intentPath, 4096)
		var original transitionRequest
		if err != nil || vault.StrictDecode(raw, &original) != nil {
			return false, renameStorageError(ctx, "read_runtime_transition_request", vault.ErrIntegrity)
		}
		if original != request {
			return false, renameError("storage_transition_pending", fmt.Sprintf("Retry the original name change from %q to %q with its preview digest and request ID.", original.Source, original.Target), map[string]any{"profile": original.Source, "request_id": original.RequestID, "facts": map[string]any{"candidate_digest": original.Digest}})
		}
	} else {
		candidate, failure := c.renameCandidateHeld(ctx, request.Source, request.Target)
		if failure != nil {
			return false, failure
		}
		if candidate.plan.Digest != request.Digest {
			return false, renameError("input_changed", "The profile changed. Preview the name change again.", nil)
		}
		if _, failure := (services.Store{Data: c.Data}).ProfileRenameRecordsHeld(ctx, "", ""); failure != nil {
			return false, failure
		}
		exposures, failure := (expose.Store{Data: c.Data}).List()
		if failure != nil {
			return false, failure
		}
		for _, item := range exposures {
			if item.State != "stopped" && item.EndedAt == nil {
				return false, renameStorageError(ctx, "check_runtime_writers", maintenance.ErrRuntimeWriterActive)
			}
		}
		if err := maintenance.VerifyRuntimeWritersHeld(ctx, c.Data, false); err != nil {
			return false, renameStorageError(ctx, "check_runtime_writers", err)
		}
		body, err := json.Marshal(request)
		if err != nil {
			return false, renameStorageError(ctx, "encode_runtime_transition_request", err)
		}
		if err := maintenance.Write(intentPath, body); err != nil {
			return false, renameStorageError(ctx, "write_runtime_transition_request", err)
		}
	}
	if err := maintenance.StageRuntimeTransitionHeld(ctx, c.Data); err != nil {
		return false, protocol.WithEffects(renameStorageError(ctx, "stage_runtime_transition", err), "partial")
	}
	if err := maintenance.PublishRuntimeTransitionHeld(ctx, c.Data, func(ctx context.Context, _ string) error {
		return maintenance.VerifyRuntimeWritersHeld(ctx, c.Data, true)
	}); err != nil {
		return false, protocol.WithEffects(renameStorageError(ctx, "publish_runtime_transition", err), "partial")
	}
	return true, nil
}
