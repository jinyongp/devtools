package values

import (
	"context"
	"errors"
	"path/filepath"
	"time"

	"github.com/jinyongp/devtools/internal/maintenance"
	"github.com/jinyongp/devtools/internal/project"
	"github.com/jinyongp/devtools/internal/protocol"
	"github.com/jinyongp/devtools/internal/vault"
)

// RequestView contains only the saved operation's metadata, never its input.
// A saved result describes the original outcome rather than current values.
type RequestView struct {
	Profile string      `json:"profile"`
	Item    RequestItem `json:"item"`
}

type RequestItem struct {
	RequestID string        `json:"request_id"`
	State     string        `json:"state"`
	Result    *ChangeResult `json:"result,omitempty"`
	CreatedAt *time.Time    `json:"created_at"`
	ExpiresAt *time.Time    `json:"expires_at"`
}

// InspectRequest borrows an existing warm session without using the write-key
// providers, starting a daemon, unlocking protection or touching idle expiry.
func (s ModernStore) InspectRequest(ctx context.Context, id string) (view RequestView, condition *protocol.Error) {
	defer func() { Recover(condition, s.root(), s.Profile, "", id, s.Invocation) }()
	out := RequestView{Profile: s.Profile, Item: RequestItem{RequestID: id}}
	if !project.ValidProfile(s.Profile) || !modernRequestID.MatchString(id) {
		return out, protocol.NewError("invalid_argument", "Provide profile and request UUID.", 2, nil)
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	directory := filepath.Join(s.root(), "vault")
	slot, err := (vault.KeyFileStore{Directory: directory}).Read()
	var master *vault.Material
	var vaultID string
	if err == nil {
		client, e := vault.OpenSessionClient(directory)
		if e != nil {
			return out, VaultError(ctx, e)
		}
		master, err = client.InspectCached(ctx)
		if err != nil {
			return out, VaultError(ctx, err)
		}
		defer master.Destroy()
		vaultID = slot.VaultID
	} else if !errors.Is(err, vault.ErrKeyMissing) {
		return out, VaultError(ctx, err)
	}
	// Acquisition recovers pending transactions before a missing-record result.
	release, err := maintenance.AcquireShared(ctx, s.root())
	if err != nil {
		return out, VaultError(ctx, err)
	}
	defer release()
	if master == nil {
		if err := vault.CheckPublicRootHeld(s.root()); err != nil {
			return out, VaultError(ctx, err)
		}
	}
	receipt, result, found, err := s.receiptHeld(ctx, master, vaultID, id)
	if err != nil {
		return out, VaultError(ctx, err)
	}
	if !found {
		return out, failure("request_not_found", "No saved result exists for this request UUID.")
	}
	if master == nil {
		key, _, err := sharedReceiptKey(ctx, s.root(), "", nil, false)
		if err != nil {
			return out, VaultError(ctx, err)
		}
		key.Destroy()
	}
	if receipt.CreatedAt != 0 {
		created := time.Unix(receipt.CreatedAt, 0).UTC()
		out.Item.CreatedAt = &created
	}
	if receipt.ExpiresAt != 0 {
		expires := time.Unix(receipt.ExpiresAt, 0).UTC()
		out.Item.ExpiresAt = &expires
	}
	out.Item.State = "completed"
	if receipt.ExpiresAt != 0 && time.Now().Unix() >= receipt.ExpiresAt {
		out.Item.State = "expired"
	} else {
		out.Item.Result = &result
	}
	return out, nil
}
