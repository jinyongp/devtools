package values

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/jinyongp/devtools/internal/platformfs"
	"os"
	"path/filepath"
	"reflect"

	"github.com/jinyongp/devtools/internal/maintenance"
	"github.com/jinyongp/devtools/internal/profilekey"
)

func (s Store) receiptRelative(requestID string) string {
	return "backup-receipts/values-" + profilekey.Key(s.Profile) + "-" + requestID + ".json"
}

// New managed receipts map exact UUID text to lowercase filenames. A UUID-shaped
// scope suffix keeps historical vault receipt enumeration/pruning compatible.
func (s ModernStore) receiptRelative(requestID string) string {
	key := valueRequestKey(requestID)
	scope := key[:32]
	id := scope[:8] + "-" + scope[8:12] + "-" + scope[12:16] + "-" + scope[16:20] + "-" + scope[20:]
	return "backup-receipts/values-" + profilekey.Key(s.Profile) + "-s2-" + key + "-" + id + ".json"
}

func valueRequestKey(id string) string {
	sum := sha256.Sum256([]byte(id))
	return hex.EncodeToString(sum[:])
}

func valueReceiptScope(id string) string { return valueRequestKey(id)[:32] }

// Ordinary file lookup can alias a differently cased historical UUID filename.
// Enumerate only when an old path exists; new receipt reads remain direct.
func exactLegacyReceipt(path string) (bool, error) {
	if _, err := os.Lstat(path); errors.Is(err, os.ErrNotExist) {
		return false, nil
	} else if err != nil {
		return false, err
	}
	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil {
		return false, err
	}
	for _, entry := range entries {
		if entry.Name() == filepath.Base(path) {
			return true, nil
		}
	}
	return false, nil
}

// readReceipt preserves replay for pre-migration requests. Conflicting copies
// are storage damage, not permission to execute the request a second time.
func (s Store) readReceipt(requestID string) (changeReceipt, bool, error) {
	canonical := s.receiptRelative(requestID)
	legacy := "backup-receipts/values-" + hex.EncodeToString([]byte(s.Profile)) + "-" + requestID + ".json"
	var result changeReceipt
	found := false
	for index, relative := range []string{canonical, legacy} {
		body, err := maintenance.Read(filepath.Join(maintenance.Root(s.Directory), relative), 1<<20)
		if errors.Is(err, os.ErrNotExist) || index == 1 && platformfs.NameTooLong(filepath.Join(maintenance.Root(s.Directory), relative), err) {
			continue
		}
		if err != nil {
			return result, false, err
		}
		var receipt changeReceipt
		if json.Unmarshal(body, &receipt) != nil || receipt.Fingerprint == "" || receipt.Result.Profile != s.Profile {
			return result, false, errors.New("invalid value receipt")
		}
		if found && !reflect.DeepEqual(result, receipt) {
			return result, false, errors.New("conflicting value receipts")
		}
		result, found = receipt, true
	}
	return result, found, nil
}
