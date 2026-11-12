package profilekey

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/jinyongp/devtools/internal/maintenance"
	"github.com/jinyongp/devtools/internal/project"
	"github.com/jinyongp/devtools/internal/protocol"
)

const RenameNamesPath = "profile-renames/names.json"
const renameNamesLimit = 4 << 20

// RenameNames records each reserved name's owner and its current address.
// Unrenamed profiles have no entry.
type RenameNames struct {
	Version int               `json:"version"`
	Owners  map[string]string `json:"owners"`
	Current map[string]string `json:"current"`
}

func (r RenameNames) valid() bool {
	if r.Version != 1 || r.Owners == nil || r.Current == nil {
		return false
	}
	for id, name := range r.Current {
		if decoded, err := hex.DecodeString(id); err != nil || len(decoded) != 16 || strings.ToLower(id) != id || !project.ValidProfile(name) || r.Owners[name] != id {
			return false
		}
	}
	for name, id := range r.Owners {
		if !project.ValidProfile(name) || r.Current[id] == "" {
			return false
		}
	}
	return true
}

// ReadRenameNamesHeld requires a data-root maintenance lease. Missing state
// means the store has never published a rename.
func ReadRenameNamesHeld(root string) (RenameNames, error) {
	dir := filepath.Join(root, "profile-renames")
	if err := privateDirectory(dir); errors.Is(err, os.ErrNotExist) {
		return RenameNames{Version: 1, Owners: map[string]string{}, Current: map[string]string{}}, nil
	} else if err != nil {
		return RenameNames{}, err
	}
	raw, err := maintenance.Read(filepath.Join(root, RenameNamesPath), renameNamesLimit)
	if errors.Is(err, os.ErrNotExist) {
		// Before-images may leave an empty private directory after rollback.
		entries, readErr := os.ReadDir(dir)
		if readErr != nil {
			return RenameNames{}, readErr
		}
		if len(entries) == 0 {
			return RenameNames{Version: 1, Owners: map[string]string{}, Current: map[string]string{}}, nil
		}
	}
	if err != nil {
		return RenameNames{}, err
	}
	var names RenameNames
	if json.Unmarshal(raw, &names) != nil || !names.valid() {
		return RenameNames{}, errors.New("invalid profile name reservations")
	}
	return names, nil
}

func (r RenameNames) Encode() ([]byte, error) {
	if !r.valid() {
		return nil, errors.New("invalid profile name reservations")
	}
	body, err := json.Marshal(r)
	if err != nil {
		return nil, err
	}
	if len(body) > renameNamesLimit {
		return nil, errors.New("profile name reservations exceed size limit")
	}
	return body, nil
}

func (r RenameNames) CurrentName(name string) string {
	if current := r.Current[r.Owners[name]]; current != "" {
		return current
	}
	return name
}

func (r RenameNames) RequireCurrent(name string) *protocol.Error {
	if current := r.CurrentName(name); current != name {
		message := fmt.Sprintf("Profile %q was renamed to %q. Select %q in the same store.", name, current, current)
		return protocol.NewError("profile_renamed", message, 3, map[string]any{"profile": name, "facts": map[string]any{"suggestions": []string{current}}})
	}
	return nil
}

// PreviousNames returns historical addresses only for a current name.
func (r RenameNames) PreviousNames(current string) []string {
	owner := r.Owners[current]
	if owner == "" || r.Current[owner] != current {
		return nil
	}
	var previous []string
	for name, id := range r.Owners {
		if id == owner && name != current {
			previous = append(previous, name)
		}
	}
	sort.Strings(previous)
	return previous
}

func RequireCurrentHeld(root, name string) *protocol.Error {
	names, err := ReadRenameNamesHeld(root)
	if err != nil {
		return renameNameError(err)
	}
	return names.RequireCurrent(name)
}

// RequireCurrentForExecution is for callers that own execution admission
// throughout the operation. Recover pending publication before choosing a name.
func RequireCurrentForExecution(ctx context.Context, root, name string) *protocol.Error {
	release, err := maintenance.AcquireQuery(ctx, root)
	if err != nil {
		if condition := protocol.ContextError(ctx.Err()); condition != nil {
			return condition
		}
		return renameNameError(err)
	}
	defer release()
	return RequireCurrentHeld(root, name)
}

func renameNameError(err error) *protocol.Error {
	return protocol.WithStorageDetails(protocol.NewError("storage_error", "Cannot read profile name reservations.", 1, nil), "profiles", "read_rename_names", "profile_store", err)
}
