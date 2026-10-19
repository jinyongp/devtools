package dashboard

import (
	"context"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/jinyongp/devtools/internal/protocol"
	"github.com/jinyongp/devtools/internal/values"
	"github.com/jinyongp/devtools/internal/vault"
)

type vaultActionRequest struct {
	Action      string `json:"action"`
	Passphrase  string `json:"passphrase,omitempty"`
	Interactive bool   `json:"interactive,omitempty"`
}

// The existing authenticated same-origin action boundary owns this operation.
// Passphrases live only in the transient request and private session frame; they
// never enter query strings, operation receipts, files or responses.
func (s *Server) vaultAction(ctx context.Context, request *vaultActionRequest) (any, *protocol.Error) {
	defer func() { request.Passphrase = "" }()
	if request.Action != "lock" && request.Action != "unlock" {
		return nil, protocol.NewError("invalid_argument", "Choose vault lock or unlock.", 2, nil)
	}
	if request.Action == "lock" && (request.Passphrase != "" || request.Interactive) {
		return nil, protocol.NewError("invalid_argument", "Lock does not accept unlock inputs.", 2, nil)
	}
	if len(request.Passphrase) > vault.MaxPassphraseBytes || !utf8.ValidString(request.Passphrase) || strings.ContainsAny(request.Passphrase, "\x00\r\n") {
		return nil, protocol.NewError("invalid_argument", "Provide a bounded single-line vault passphrase.", 2, nil)
	}
	directory := filepath.Join(filepath.Dir(s.data), "vault")
	slot, err := (vault.KeyFileStore{Directory: directory}).Read()
	if err != nil {
		return nil, values.VaultError(ctx, err)
	}
	var pass *vault.Material
	if request.Action == "unlock" {
		if slot.Backend == "passphrase" {
			if request.Passphrase == "" {
				return nil, protocol.NewError("passphrase_required", "Enter the vault passphrase.", 3, nil)
			}
			raw := []byte(request.Passphrase)
			pass = vault.NewMaterial(raw)
			clear(raw)
			request.Passphrase = ""
			defer pass.Destroy()
		} else if request.Passphrase != "" {
			request.Passphrase = ""
			return nil, protocol.NewError("invalid_argument", "This vault uses system protection.", 2, nil)
		}
	}
	client, err := (vault.SessionManager{Directory: directory}).Client(ctx)
	if err != nil {
		return nil, values.VaultError(ctx, err)
	}
	if request.Action == "unlock" {
		err = client.Unlock(ctx, pass, request.Interactive)
	} else {
		err = client.Lock(ctx)
	}
	if err != nil {
		return nil, values.VaultError(ctx, err)
	}
	status, err := client.Status(ctx)
	if err != nil {
		return nil, values.VaultError(ctx, err)
	}
	return map[string]any{"item": status}, nil
}
