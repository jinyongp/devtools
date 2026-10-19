package cli

import (
	"context"
	"errors"
	"os"
	"path/filepath"

	"github.com/jinyongp/devtools/internal/maintenance"
	"github.com/jinyongp/devtools/internal/protocol"
	"github.com/jinyongp/devtools/internal/values"
	"github.com/jinyongp/devtools/internal/vault"
)

func (a *App) keySessionClient(ctx context.Context, directory string) (*vault.SessionClient, error) {
	if a.vaultClient != nil {
		return a.vaultClient(ctx, directory)
	}
	return (vault.SessionManager{Directory: directory}).Client(ctx)
}
func (a *App) vaultDirectory() (string, *protocol.Error) {
	directory, e := a.dataDirectory()
	if e != nil {
		return "", e
	}
	return filepath.Join(filepath.Dir(directory), "vault"), nil
}
func vaultStatusSchema() map[string]any {
	return object(map[string]any{"state": map[string]any{"enum": []string{"uninitialized", "locked", "unlocked", "locking", "locked_manual", "recovery_required"}}, "vault_id": stringSchema(), "backend": stringSchema(), "idle_expires_at": stringSchema()}, "state")
}
func vaultPassphraseOptions() []Option {
	return []Option{filePathOption("passphrase-file", false), {Name: "passphrase-stdin", Boolean: true, Description: "Read the vault passphrase from piped stdin; never pass it in argv or environment."}}
}
func vaultPassphraseRequired(action string) *protocol.Error {
	return protocol.NewError("passphrase_required", "A vault passphrase is required.", 3, map[string]any{"remedies": []protocol.Remedy{{Argv: []string{"devtools", "vault", action, "--passphrase-file", "PATH"}, RequiredInputs: []string{"passphrase-file"}, Message: "Provide the vault passphrase through a private regular file."}}})
}
func (a *App) vaultPassphrase(ctx context.Context, streams IO, options map[string]string, action string, confirm bool) (*vault.Material, *protocol.Error) {
	hasFile := options["passphrase-file"] != ""
	stdin := options["passphrase-stdin"] == "true"
	if hasFile && stdin {
		return nil, argumentError("Select exactly one passphrase input.", "")
	}
	var text string
	var e *protocol.Error
	if hasFile || stdin {
		text, e = readPassphraseSource(ctx, streams, options["passphrase-file"], stdin, action)
	} else {
		if a.passphrasePrompt == nil {
			return nil, vaultPassphraseRequired(action)
		}
		text, e = a.passphrasePrompt(ctx, streams, "Vault passphrase: ")
		if e == nil && confirm {
			var repeated string
			repeated, e = a.passphrasePrompt(ctx, streams, "Confirm vault passphrase: ")
			if e == nil && text != repeated {
				e = protocol.NewError("passphrase_mismatch", "Vault passphrases do not match.", 3, nil)
			}
		}
	}
	if e != nil {
		if e.Code == "passphrase_required" {
			return nil, vaultPassphraseRequired(action)
		}
		return nil, e
	}
	if text == "" {
		return nil, vaultPassphraseRequired(action)
	}
	return vault.NewMaterial([]byte(text)), nil
}
func (a *App) registerVault() {
	a.commands = append(a.commands, Command{Name: "vault status", Description: "Inspect vault protection and key-session state without unlocking or extending its idle timeout.", Options: []Option{}, Output: itemOutput(vaultStatusSchema()), Run: func(ctx context.Context, _ IO, _ Request) (any, *protocol.Error) {
		directory, e := a.vaultDirectory()
		if e != nil {
			return nil, e
		}
		status, err := vault.InspectSession(ctx, directory)
		if err != nil {
			return nil, values.VaultError(ctx, err)
		}
		return map[string]any{"item": status}, nil
	}})
	initOptions := append(vaultPassphraseOptions(), Option{Name: "backend", Default: "native", Pattern: "^(native|passphrase)$", Description: "Use existing OS protection or explicitly choose a passphrase session."}, Option{Name: "interactive", Boolean: true, Description: "Allow native OS authentication UI for this explicit setup operation."})
	a.commands = append(a.commands, Command{Name: "vault init", Description: "Initialize local key protection and safely migrate all legacy value profiles; original user files remain intact.", Options: initOptions, Output: changedItemOutput(vaultStatusSchema()), Run: a.initializeVault})
	a.commands = append(a.commands, Command{Name: "vault recover", Description: "Finish an encrypted storage transition or a setup interrupted before migration; requires an unlocked session.", Options: []Option{}, Output: changedItemOutput(vaultStatusSchema()), Run: a.recoverVault})
	for _, action := range []string{"unlock", "lock"} {
		options := []Option{}
		if action == "unlock" {
			options = append(vaultPassphraseOptions(), Option{Name: "interactive", Boolean: true, Description: "Allow native OS authentication UI for this explicit unlock operation."})
		}
		a.commands = append(a.commands, Command{Name: "vault " + action, Description: action + " the reusable vault key session. Lock cannot revoke environments already delivered to applications.", Options: options, Output: changedItemOutput(vaultStatusSchema()), Run: func(ctx context.Context, streams IO, r Request) (any, *protocol.Error) {
			return a.changeVaultLock(ctx, streams, r, action)
		}})
	}
}
func (a *App) initializeVault(ctx context.Context, streams IO, r Request) (any, *protocol.Error) {
	directory, e := a.vaultDirectory()
	if e != nil {
		return nil, e
	}
	keys := vault.KeyFileStore{Directory: directory}
	if _, err := keys.Read(); err == nil {
		return nil, protocol.NewError("vault_already_initialized", "Vault already exists; unlock it or recover the pending migration.", 3, nil)
	} else if !errors.Is(err, vault.ErrKeyMissing) {
		return nil, values.VaultError(ctx, err)
	}
	id, err := vault.NewID()
	if err != nil {
		return nil, values.VaultError(ctx, err)
	}
	backend := r.Options["backend"]
	var protector vault.Protector
	var pass *vault.Material
	if backend == "passphrase" {
		pass, e = a.vaultPassphrase(ctx, streams, r.Options, "init", true)
		if e != nil {
			return nil, e
		}
		defer pass.Destroy()
		protector = vault.PassphraseProtector{VaultID: id, Passphrase: pass}
	} else {
		if r.Options["passphrase-file"] != "" || r.Options["passphrase-stdin"] == "true" {
			return nil, argumentError("Passphrase inputs require --backend passphrase.", "backend")
		}
		dirs, pathErr := a.currentPaths()
		if pathErr != nil {
			return nil, protocol.NewError("io_error", "Cannot resolve user directories.", 1, nil)
		}
		if dirs.IsolatedRoot != "" {
			return nil, protocol.NewError("isolated_operation_forbidden", "Native key-store mutation is unavailable in an isolated root; explicitly use passphrase protection.", 3, nil)
		}
		backend = vault.NativeBackend()
		protector, err = vault.NativeProtection(id, r.Options["interactive"] == "true")
		if err != nil {
			return nil, values.VaultError(ctx, err)
		}
	}
	master, err := keys.Initialize(ctx, id, backend, protector)
	if err != nil {
		return nil, values.VaultError(ctx, err)
	}
	defer master.Destroy()
	inventory, err := values.InspectLegacyMigration(ctx, filepath.Dir(directory), master)
	if err == nil {
		operation, randomErr := vault.NewID()
		if randomErr != nil {
			return nil, values.VaultError(ctx, randomErr)
		}
		_, err = values.MigrateLegacy(ctx, filepath.Dir(directory), id, master, operation, inventory.Fingerprint)
	}
	if err != nil {
		return nil, values.VaultError(ctx, err)
	}
	if err = (vault.FileStore{Root: filepath.Dir(directory), VaultID: id, CatalogKey: master}).VerifyStorage(ctx); err != nil {
		return nil, values.VaultError(ctx, err)
	}
	client, err := a.keySessionClient(ctx, directory)
	if err != nil {
		return nil, values.VaultError(ctx, err)
	}
	if err = client.Unlock(ctx, pass, r.Options["interactive"] == "true"); err != nil {
		return nil, values.VaultError(ctx, err)
	}
	status, err := client.Status(ctx)
	if err != nil {
		return nil, values.VaultError(ctx, err)
	}
	return map[string]any{"item": status, "changed": true}, nil
}
func (a *App) changeVaultLock(ctx context.Context, streams IO, r Request, action string) (any, *protocol.Error) {
	directory, e := a.vaultDirectory()
	if e != nil {
		return nil, e
	}
	slot, err := (vault.KeyFileStore{Directory: directory}).Read()
	if err != nil {
		return nil, values.VaultError(ctx, err)
	}
	var pass *vault.Material
	if action == "unlock" {
		if slot.Backend == "passphrase" {
			pass, e = a.vaultPassphrase(ctx, streams, r.Options, "unlock", false)
			if e != nil {
				return nil, e
			}
			defer pass.Destroy()
		} else if r.Options["passphrase-file"] != "" || r.Options["passphrase-stdin"] == "true" {
			return nil, argumentError("This vault uses native protection, not a passphrase.", "")
		}
	}
	client, err := a.keySessionClient(ctx, directory)
	if err != nil {
		return nil, values.VaultError(ctx, err)
	}
	before, err := client.Status(ctx)
	if err != nil {
		return nil, values.VaultError(ctx, err)
	}
	if action == "unlock" {
		err = client.Unlock(ctx, pass, r.Options["interactive"] == "true")
	} else {
		err = client.Lock(ctx)
	}
	if err != nil {
		return nil, values.VaultError(ctx, err)
	}
	after, err := client.Status(ctx)
	if err != nil {
		return nil, values.VaultError(ctx, err)
	}
	return map[string]any{"item": after, "changed": before.State != after.State}, nil
}

func (a *App) recoverVault(ctx context.Context, _ IO, _ Request) (any, *protocol.Error) {
	directory, e := a.vaultDirectory()
	if e != nil {
		return nil, e
	}
	client, err := a.keySessionClient(ctx, directory)
	if err != nil {
		return nil, values.VaultError(ctx, err)
	}
	master, err := client.Use(ctx)
	if err != nil {
		return nil, values.VaultError(ctx, err)
	}
	defer master.Destroy()
	slot, err := (vault.KeyFileStore{Directory: directory}).Read()
	if err != nil {
		return nil, values.VaultError(ctx, err)
	}
	root := filepath.Dir(directory)
	finish, err := vault.AcquireLaunchMutation(ctx, directory)
	if err != nil {
		return nil, values.VaultError(ctx, err)
	}
	release, gateErr := maintenance.AcquireExclusive(ctx, root)
	if release != nil {
		release()
	}
	finish()
	changed := false
	if gateErr != nil {
		result, err := (vault.ForwardStore{Root: root, VaultID: slot.VaultID, Master: master}).Recover(ctx)
		if err != nil {
			return nil, values.VaultError(ctx, err)
		}
		changed = result.Committed
	}
	if _, err = os.Lstat(filepath.Join(root, "profiles", "vault-catalog.json")); errors.Is(err, os.ErrNotExist) {
		inventory, err := values.InspectLegacyMigration(ctx, root, master)
		if err != nil {
			return nil, values.VaultError(ctx, err)
		}
		operation, err := vault.NewID()
		if err != nil {
			return nil, values.VaultError(ctx, err)
		}
		if _, err = values.MigrateLegacy(ctx, root, slot.VaultID, master, operation, inventory.Fingerprint); err != nil {
			return nil, values.VaultError(ctx, err)
		}
		changed = true
	} else if err != nil {
		return nil, values.VaultError(ctx, vault.ErrRecovery)
	}
	if err = (vault.FileStore{Root: root, VaultID: slot.VaultID, CatalogKey: master}).VerifyStorage(ctx); err != nil {
		return nil, values.VaultError(ctx, err)
	}
	status, err := client.Status(ctx)
	if err != nil {
		return nil, values.VaultError(ctx, err)
	}
	return map[string]any{"item": status, "changed": changed}, nil
}
