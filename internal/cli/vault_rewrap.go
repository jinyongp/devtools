package cli

import (
	"context"
	"path/filepath"
	"strings"

	"github.com/jinyongp/devtools/internal/protocol"
	"github.com/jinyongp/devtools/internal/values"
	"github.com/jinyongp/devtools/internal/vault"
)

func rewrapInfoSchema() map[string]any {
	backend := map[string]any{"enum": []string{"passphrase", "keychain", "secret-service", "dpapi"}}
	return object(map[string]any{"request_id": stringSchema(), "from_backend": backend, "backend": backend,
		"slot_published": map[string]any{"type": "boolean"}, "completed": map[string]any{"type": "boolean"},
		"integrity_verified": map[string]any{"const": false}}, "request_id", "slot_published", "completed", "integrity_verified")
}

func (a *App) registerVaultRewrap() {
	options := []Option{
		{Name: "from-backend", Required: true, Pattern: "^(native|passphrase)$", Description: "Original protection; retain this selection when resuming after slot publication."},
		{Name: "backend", Required: true, Pattern: "^(native|passphrase)$", Description: "Explicit target protection for the same root key; never changes provider credentials."},
		{Name: "request-id", Required: true, Pattern: uuidPattern, Description: "New UUID for a new change; reuse it and the same target credentials after interruption."},
		{Name: "interactive", Boolean: true, Description: "Allow native OS authentication UI for this explicit protection change."},
	}
	for _, prefix := range []string{"current", "new"} {
		options = append(options, filePathOption(prefix+"-passphrase-file", false), Option{Name: prefix + "-passphrase-stdin", Boolean: true,
			Description: "Read this passphrase from piped stdin. Only one side may consume stdin; never use argv or environment."})
	}
	a.commands = append(a.commands, Command{Name: "vault rewrap", Description: "Change protection of the same vault root key. Authenticate the original slot before publication, the target slot when resuming, and retain the original request UUID.",
		Options: options, Output: object(map[string]any{"item": vaultStatusSchema(), "request_id": stringSchema(), "changed": map[string]any{"type": "boolean"}, "replayed": map[string]any{"type": "boolean"}}, "item", "request_id", "changed", "replayed"), Run: a.rewrapVault})
}

func rewrapPublicBackend(backend string) string {
	if backend == "passphrase" {
		return backend
	}
	return "native"
}

func rewrapRemedy(scope protocol.InvocationContext, requestID, from, target string, needCurrent bool) protocol.Remedy {
	argv := []string{"devtools", "vault", "rewrap", "--request-id", requestID}
	inputs := []string{}
	for _, pair := range [][2]string{{"from-backend", from}, {"backend", target}} {
		if pair[1] == "" {
			inputs = append(inputs, pair[0])
		} else {
			argv = append(argv, "--"+pair[0], rewrapPublicBackend(pair[1]))
		}
	}
	if needCurrent && from == "passphrase" {
		inputs = append(inputs, "current-passphrase-file")
	}
	if target == "passphrase" {
		inputs = append(inputs, "new-passphrase-file")
	}
	return protocol.LocalAction(argv, inputs, "Resume this protection change with the original UUID and target credentials. Before slot publication also provide the original credentials; native authentication may require explicit --interactive. Never delete the slot or reinitialize the vault.", "value_write").Remedy(scope)
}

func pendingRewrapError(info *vault.RewrapInfo, scope protocol.InvocationContext) *protocol.Error {
	return protocol.NewError(vault.ErrRecovery.Error(), "A vault protection change is pending. Resume the original rewrap; storage recovery cannot choose replacement protection.", 3,
		map[string]any{"rewrap": info, "remedies": []protocol.Remedy{rewrapRemedy(scope, info.RequestID, info.FromBackend, info.Backend, !info.SlotPublished)}})
}

func (a *App) rewrapPassphrase(ctx context.Context, streams IO, options map[string]string, prefix string, required, confirm bool) (*vault.Material, *protocol.Error) {
	file, stdin := options[prefix+"-passphrase-file"], options[prefix+"-passphrase-stdin"] == "true"
	if file != "" && stdin {
		return nil, argumentError("Select one input for the "+prefix+" passphrase.", prefix+"-passphrase-file")
	}
	// After publication the retired protector is only an identity. Recovery must
	// not require an old private file or OS item that no longer exists.
	if !required {
		return nil, nil
	}
	var text string
	var e *protocol.Error
	if file != "" || stdin {
		text, e = readPassphraseSource(ctx, streams, file, stdin, "rewrap")
	} else if a.passphrasePrompt != nil {
		text, e = a.passphrasePrompt(ctx, streams, prefix+" vault passphrase: ")
		if e == nil && confirm {
			var repeat string
			repeat, e = a.passphrasePrompt(ctx, streams, "Confirm new vault passphrase: ")
			if e == nil && text != repeat {
				e = protocol.NewError("passphrase_mismatch", "Vault passphrases do not match.", 3, nil)
			}
		}
	}
	if e != nil && e.Code != "passphrase_required" {
		return nil, e
	}
	if e != nil || text == "" {
		return nil, protocol.NewError("passphrase_required", "Provide the "+prefix+" vault passphrase with --"+prefix+"-passphrase-file or --"+prefix+"-passphrase-stdin, or use a terminal prompt.", 3, nil)
	}
	return vault.NewMaterial([]byte(text)), nil
}

func (a *App) rewrapVault(ctx context.Context, streams IO, r Request) (result any, condition *protocol.Error) {
	directory, e := a.vaultDirectory()
	if e != nil {
		return nil, e
	}
	keys := vault.KeyFileStore{Directory: directory}
	id := strings.ReplaceAll(strings.ToLower(r.Options["request-id"]), "-", "")
	from, target := r.Options["from-backend"], r.Options["backend"]
	attempted := false
	defer func() {
		values.Recover(condition, filepath.Dir(directory), "", "", "", r.Invocation)
		if condition == nil {
			return
		}
		if condition.Details == nil {
			condition.Details = map[string]any{}
		}
		if attempted {
			protocol.WithEffects(condition, "unknown")
		}
		if condition.Code == "isolated_operation_forbidden" {
			condition.Details["remedies"] = []protocol.Remedy{protocol.LocalAction([]string{"devtools", "vault", "status"}, nil, "Inspect this isolated vault; native protection changes require the actual host store.", "read").Remedy(r.Invocation)}
			return
		}
		if pending, _ := keys.RewrapInfo(""); pending != nil {
			condition.Details["rewrap"] = pending
			condition.Details["remedies"] = []protocol.Remedy{rewrapRemedy(r.Invocation, pending.RequestID, pending.FromBackend, pending.Backend, !pending.SlotPublished)}
		} else {
			condition.Details["remedies"] = []protocol.Remedy{rewrapRemedy(r.Invocation, r.Options["request-id"], from, target, true)}
		}
	}()
	if r.Options["current-passphrase-stdin"] == "true" && r.Options["new-passphrase-stdin"] == "true" {
		return nil, argumentError("Only one passphrase may consume stdin.", "new-passphrase-stdin")
	}
	for _, side := range [][2]string{{"current", from}, {"new", target}} {
		prefix, backend := side[0], side[1]
		if backend != "passphrase" && (r.Options[prefix+"-passphrase-file"] != "" || r.Options[prefix+"-passphrase-stdin"] == "true") {
			return nil, argumentError("Passphrase input requires passphrase protection on that side.", prefix+"-passphrase-file")
		}
		if r.Options[prefix+"-passphrase-file"] != "" && r.Options[prefix+"-passphrase-stdin"] == "true" {
			return nil, argumentError("Select one passphrase input per side.", prefix+"-passphrase-file")
		}
	}
	if from == "native" || target == "native" {
		dirs, err := a.currentPaths()
		if err != nil {
			return nil, protocol.NewError("io_error", "Cannot resolve user directories.", 1, nil)
		}
		if dirs.IsolatedRoot != "" {
			return nil, protocol.NewError("isolated_operation_forbidden", "Native key-store mutation is unavailable in an isolated root.", 3, nil)
		}
	}
	slot, err := keys.Read()
	if err != nil {
		return nil, values.VaultError(ctx, err)
	}
	info, err := keys.RewrapInfo(id)
	if err != nil {
		return nil, values.VaultError(ctx, err)
	}
	needCurrent := info == nil || (!info.SlotPublished && !info.Completed)
	var oldPass, newPass *vault.Material
	if from == "passphrase" {
		oldPass, e = a.rewrapPassphrase(ctx, streams, r.Options, "current", needCurrent, false)
		if e != nil {
			return nil, e
		}
		defer oldPass.Destroy()
	}
	if target == "passphrase" {
		newPass, e = a.rewrapPassphrase(ctx, streams, r.Options, "new", true, info == nil)
		if e != nil {
			return nil, e
		}
		defer newPass.Destroy()
	}
	protector := func(backend string, pass *vault.Material) (vault.Protector, error) {
		if backend == "passphrase" {
			return vault.PassphraseProtector{VaultID: slot.VaultID, Passphrase: pass}, nil
		}
		return vault.NativeProtection(slot.VaultID, r.Options["interactive"] == "true")
	}
	old, err := protector(from, oldPass)
	if err != nil {
		return nil, values.VaultError(ctx, err)
	}
	next, err := protector(target, newPass)
	if err != nil {
		return nil, values.VaultError(ctx, err)
	}
	// Upgrade/retire an older protocol before changing the slot, so a cached
	// daemon using older ticket rules cannot survive successful publication.
	client, err := a.keySessionClient(ctx, directory)
	if err != nil {
		return nil, values.VaultError(ctx, err)
	}
	attempted = true
	changed, err := keys.Rewrap(ctx, id, old, next)
	if err != nil {
		return nil, values.VaultError(ctx, err)
	}
	status, err := client.Status(ctx)
	if err != nil {
		return nil, values.VaultError(ctx, err)
	}
	return map[string]any{"item": status, "request_id": strings.ToLower(r.Options["request-id"]), "changed": changed.Changed, "replayed": changed.Replayed}, nil
}
