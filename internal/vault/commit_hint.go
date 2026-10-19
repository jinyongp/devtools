package vault

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"

	"github.com/jinyongp/devtools/internal/maintenance"
)

// CommitHint is deliberately unauthenticated advisory metadata. Consumers must
// authenticate current storage before comparing inputs or taking an action.
// One shared latest hint bounds storage regardless of commit/profile count.
type CommitHint struct {
	Format     int    `json:"format"`
	Profile    string `json:"profile"`
	ProfileID  string `json:"profile_id"`
	Generation string `json:"storage_generation"`
	Revision   uint64 `json:"revision"`
	KeyEpoch   uint64 `json:"key_epoch"`
}

func (h CommitHint) valid() bool {
	return h.Format == 1 && namePattern.MatchString(h.Profile) && validID(h.ProfileID) && validID(h.Generation) && h.Revision > 0
}

// AddCommitHint joins the same transaction as an already validated profile.
// Notification delivery is optional; consumers reconcile even without this file.
func AddCommitHint(files map[string][]byte, p *Profile) error {
	if files == nil || p == nil {
		return ErrInvalid
	}
	hint := CommitHint{Format: 1, Profile: p.Name, ProfileID: p.ID, Generation: p.Generation, Revision: p.Revision, KeyEpoch: p.KeyEpoch}
	if !hint.valid() {
		return ErrInvalid
	}
	if _, exists := files[maintenance.ValueChangeHintPath]; exists {
		return ErrInvalid
	}
	body, err := json.Marshal(hint)
	if err != nil {
		return ErrInvalid
	}
	files[maintenance.ValueChangeHintPath] = body
	return nil
}

// ReadCommitHint neither creates files nor obtains a key. Absent/invalid hints
// cannot suppress periodic reconciliation and are never reported as verified.
func ReadCommitHint(root string) (*CommitHint, error) {
	body, err := maintenance.Read(filepath.Join(root, maintenance.ValueChangeHintPath), 4096)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, ErrRecovery
	}
	defer clear(body)
	var hint CommitHint
	if StrictDecode(body, &hint) != nil || !hint.valid() {
		return nil, ErrInvalid
	}
	return &hint, nil
}
