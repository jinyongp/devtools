package archiveproof

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/jinyongp/devtools/internal/maintenance"
	"github.com/jinyongp/devtools/internal/project"
)

type Proof struct {
	ArchiveID  string
	Profile    string
	ArchivedAt time.Time
}

type archiveEntry struct {
	ID         string     `json:"id"`
	Kind       string     `json:"kind"`
	Profile    string     `json:"profile"`
	Source     string     `json:"source"`
	Bytes      int64      `json:"bytes"`
	ArchivedAt time.Time  `json:"archived_at"`
	RestoredAt *time.Time `json:"restored_at"`
	PurgedAt   *time.Time `json:"purged_at"`
}

func validID(id string) bool {
	b, err := hex.DecodeString(strings.ReplaceAll(id, "-", ""))
	return err == nil && len(b) == 16 && len(id) == 36 && id[8] == '-' && id[13] == '-' && id[18] == '-' && id[23] == '-'
}

func privateDirectory(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode().Perm()&0077 != 0 {
		return errors.New("private archive directory required")
	}
	return nil
}

func readEntry(path string, entry *archiveEntry) error {
	body, err := maintenance.Read(path, 1<<20)
	if err != nil {
		return err
	}
	return json.Unmarshal(body, entry)
}

// CompletedProcesses returns legacy completed-process archive proofs only for
// the requested execution IDs. Malformed or unrelated archive entries are not
// proofs; a caller decides whether an execution hole without a proof is corrupt.
func CompletedProcesses(data string, executionIDs []string) (map[string]Proof, error) {
	result := map[string]Proof{}
	if len(executionIDs) == 0 {
		return result, nil
	}
	wanted := map[string]string{}
	for _, id := range executionIDs {
		wanted[filepath.Join(data, "processes", id, "record.json")] = id
	}
	root := filepath.Join(data, "archives")
	if err := privateDirectory(root); errors.Is(err, os.ErrNotExist) {
		return result, nil
	} else if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, err
	}
	candidates := map[string][]Proof{}
	for _, item := range entries {
		archiveID := item.Name()
		if !validID(archiveID) {
			continue
		}
		dir := filepath.Join(root, archiveID)
		if err := privateDirectory(dir); err != nil {
			continue
		}
		var entry archiveEntry
		if err := readEntry(filepath.Join(dir, "entry.json"), &entry); err != nil {
			continue
		}
		executionID, ok := wanted[entry.Source]
		if !ok || entry.ID != archiveID || entry.Kind != "completed_process" || !project.ValidProfile(entry.Profile) || entry.ArchivedAt.IsZero() || entry.RestoredAt != nil {
			continue
		}
		candidates[executionID] = append(candidates[executionID], Proof{ArchiveID: archiveID, Profile: entry.Profile, ArchivedAt: entry.ArchivedAt})
	}
	for executionID, proofs := range candidates {
		sort.Slice(proofs, func(i, j int) bool {
			if proofs[i].ArchivedAt.Equal(proofs[j].ArchivedAt) {
				return proofs[i].ArchiveID < proofs[j].ArchiveID
			}
			return proofs[i].ArchivedAt.Before(proofs[j].ArchivedAt)
		})
		profile := proofs[0].Profile
		for _, proof := range proofs[1:] {
			if proof.Profile != profile {
				return nil, errors.New("conflicting completed-process archive proofs")
			}
		}
		result[executionID] = proofs[0]
	}
	return result, nil
}
