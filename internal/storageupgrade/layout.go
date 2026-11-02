package storageupgrade

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strconv"

	"github.com/jinyongp/devtools/internal/platformfs"
	"github.com/jinyongp/devtools/internal/profilekey"
)

// Layout inspects only storage headers and addresses. Domain adapters verify the
// payload and authentication before committing any conversion.
func Layout(domain string) Adapter {
	return Adapter{Name: domain, InspectHeld: func(ctx context.Context, root string) ([]Step, error) {
		directory := filepath.Join(root, domain)
		if domain == "values" {
			directory = filepath.Join(root, "profiles")
		}
		if err := platformfs.CheckDirectory(directory); errors.Is(err, os.ErrNotExist) {
			return []Step{}, nil
		} else if err != nil {
			return nil, err
		}
		names, err := profilekey.Enumerate(directory, domain)
		if err != nil {
			return nil, err
		}
		items := make([]Step, 0, len(names))
		for _, name := range names {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			step, err := ProfileLayout(directory, name, domain)
			if err != nil {
				return nil, err
			}
			items = append(items, step)
		}
		return items, nil
	}}
}

func ProfileLayout(directory, profile, domain string) (Step, error) {
	resolution, err := profilekey.Resolve(directory, profile, domain)
	if err != nil {
		return Step{}, err
	}
	raw, err := profilekey.MigrationSnapshot(directory, profile, domain, 128<<20)
	if err != nil {
		return Step{}, err
	}
	defer clear(raw)
	var header struct {
		Version     int    `json:"version"`
		VaultFormat int    `json:"vault_format"`
		Marker      string `json:"storage_marker"`
	}
	if len(raw) == 0 || json.Unmarshal(raw, &header) != nil {
		return Step{}, errors.New("invalid storage header")
	}
	sum := sha256.Sum256(raw)
	step := Step{Domain: domain, Profile: profile, State: "current", Fingerprint: strconv.Itoa(int(resolution.Mode)) + ":" + hex.EncodeToString(sum[:])}
	if domain == "values" {
		step.Source, step.Target = "values-v"+strconv.Itoa(header.VaultFormat), "values-v1"
		if header.VaultFormat == 0 && header.Version == 1 {
			step.Source, step.State = "legacy-values-v1", "migration_required"
		} else if header.VaultFormat != 1 {
			step.State, step.Reason = "unsupported", "unsupported_format"
		}
	} else if domain == "tasks" {
		step.Source, step.Target = header.Marker, "task-v3"
		if header.Marker == "" && (header.Version == 1 || header.Version == 2) {
			step.Source, step.State = "journal-v"+strconv.Itoa(header.Version), "migration_required"
		} else if header.Marker != "task-v3" {
			step.State, step.Reason = "unsupported", "unsupported_format"
		}
	} else {
		return Step{}, errors.New("unsupported storage domain")
	}
	if resolution.Mode == profilekey.ModeLegacy && step.State == "current" {
		step.State, step.Reason = "migration_required", "profile_address"
	}
	return step, nil
}
