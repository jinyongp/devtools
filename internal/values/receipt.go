package values

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"syscall"

	"github.com/jinyongp/devtools/internal/maintenance"
	"github.com/jinyongp/devtools/internal/profilekey"
)

func (s Store) receiptRelative(requestID string) string {
	return "backup-receipts/values-" + profilekey.Key(s.Profile) + "-" + requestID + ".json"
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
		if errors.Is(err, os.ErrNotExist) || index == 1 && errors.Is(err, syscall.ENAMETOOLONG) {
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
