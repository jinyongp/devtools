package storageupgrade

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"path/filepath"

	"github.com/jinyongp/devtools/internal/maintenance"
)

// RetainRecoveryHeld preserves committed source bytes before a domain publishes
// its replacement. Files stay private and outside the ordinary reader namespace;
// the content address makes retries retain the same recovery image.
func RetainRecoveryHeld(root, domain, profile string, files map[string][]byte) error {
	image := struct {
		Version int               `json:"version"`
		Domain  string            `json:"domain"`
		Profile string            `json:"profile"`
		Files   map[string][]byte `json:"files"`
	}{1, domain, profile, files}
	raw, err := json.Marshal(image)
	if err != nil {
		return err
	}
	defer clear(raw)
	sum := sha256.Sum256(raw)
	return maintenance.Write(filepath.Join(root, ".storage-upgrade", "recovery", hex.EncodeToString(sum[:])+".json"), raw)
}
