package services

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/jinyongp/devtools/internal/maintenance"
	"github.com/jinyongp/devtools/internal/storageupgrade"
	"github.com/jinyongp/devtools/internal/tasks"
)

func processStorageStep(root, relative string) (storageupgrade.Step, error) {
	step := storageupgrade.Step{Domain: "processes", Resource: relative, Source: "process-v2", Target: "process-v2", State: "current"}
	raw, err := maintenance.Read(filepath.Join(root, "processes", relative), 128<<20)
	if err != nil {
		return step, err
	}
	step.Fingerprint = fmt.Sprintf("%x", sha256.Sum256(raw))
	var header struct {
		Version int `json:"storage_version"`
	}
	if json.Unmarshal(raw, &header) != nil {
		return step, errors.New("invalid process document")
	}
	if header.Version == 2 {
		if filepath.Base(relative) == "record.json" {
			d, err := readDiskRecord(filepath.Join(root, "processes", relative))
			if err != nil {
				return step, err
			}
			if !validRecord(filepath.Base(filepath.Dir(relative)), d.Metadata) {
				return step, errors.New("invalid process index")
			}
			if _, err := (Store{Data: root}).RecordBytes(d.Metadata.ID); err != nil {
				return step, err
			}
		} else {
			var r receipt
			if err := (Store{Data: root}).readReceipt(filepath.Join(root, "processes", relative), &r); err != nil {
				return step, err
			}
		}
		return step, nil
	}
	if header.Version != 0 {
		step.State, step.Reason = "unsupported", "unsupported_format"
		return step, nil
	}
	if filepath.Base(relative) == "record.json" {
		var r Record
		if err := tasks.ReadPrivate(filepath.Join(root, "processes", relative), &r); err != nil || !validRecord(filepath.Base(filepath.Dir(relative)), r) {
			return step, errors.New("invalid legacy process record")
		}
		step.Profile = r.Profile
		if !(Store{Data: root}).leaseFree(context.Background(), r.ID) {
			step.State, step.Reason = "blocked", "active_writer"
			return step, nil
		}
	} else {
		var r receipt
		if err := tasks.ReadPrivate(filepath.Join(root, "processes", relative), &r); err != nil || len(r.Fingerprint) != 64 {
			return step, errors.New("invalid legacy process receipt")
		}
	}
	step.Source, step.State, step.Reason = "process-v1", "migration_required", "metadata_and_shared_body"
	return step, nil
}

func StorageUpgradeAdapter() storageupgrade.Adapter {
	return storageupgrade.Adapter{Name: "processes", InspectHeld: func(ctx context.Context, root string) ([]storageupgrade.Step, error) {
		items := []storageupgrade.Step{}
		entries, err := os.ReadDir(filepath.Join(root, "processes"))
		if errors.Is(err, os.ErrNotExist) {
			return items, nil
		}
		if err != nil {
			return nil, err
		}
		for _, entry := range entries {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			if !validID(entry.Name()) {
				continue
			}
			rel := filepath.Join(entry.Name(), "record.json")
			step, err := processStorageStep(root, rel)
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			if err != nil {
				return nil, err
			}
			items = append(items, step)
		}
		entries, err = os.ReadDir(filepath.Join(root, "processes", "receipts"))
		if errors.Is(err, os.ErrNotExist) {
			return items, nil
		}
		if err != nil {
			return nil, err
		}
		for _, entry := range entries {
			if !strings.HasSuffix(entry.Name(), ".json") || !validID(strings.TrimSuffix(entry.Name(), ".json")) {
				continue
			}
			step, err := processStorageStep(root, filepath.Join("receipts", entry.Name()))
			if err != nil {
				return nil, err
			}
			items = append(items, step)
		}
		return items, nil
	}, Apply: func(ctx context.Context, root string, step storageupgrade.Step) (bool, error) {
		s := Store{Data: root}
		unlock, err := s.Lock(ctx)
		if err != nil {
			return false, err
		}
		defer unlock()
		release, err := maintenance.AcquireExclusive(ctx, root)
		if err != nil {
			return false, err
		}
		defer release()
		current, err := processStorageStep(root, step.Resource)
		if err != nil {
			return false, err
		}
		if current.State == "current" {
			return false, nil
		}
		if current.Fingerprint != step.Fingerprint || current.State != "migration_required" {
			return false, errors.New("process storage changed during upgrade")
		}
		path := filepath.Join(root, "processes", step.Resource)
		if filepath.Base(path) == "record.json" {
			var r Record
			if err := tasks.ReadPrivate(path, &r); err != nil {
				return false, err
			}
			err = writeRecordFormat(path, r)
		} else {
			var r receipt
			if err := tasks.ReadPrivate(path, &r); err != nil {
				return false, err
			}
			err = writeReceiptFormat(path, r)
		}
		return err == nil, err
	}}
}
