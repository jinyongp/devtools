package services

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"time"

	"github.com/jinyongp/devtools/internal/maintenance"
	"github.com/jinyongp/devtools/internal/retainedbody"
	"github.com/jinyongp/devtools/internal/storageupgrade"
	"github.com/jinyongp/devtools/internal/tasks"
)

// Metadata is sufficient for navigation. The immutable body is shared with
// request results and manual archives, whose replay/restore lifetimes differ.
type diskRecord struct {
	Version   int                    `json:"storage_version"`
	Metadata  Record                 `json:"metadata"`
	Body      retainedbody.Reference `json:"body"`
	RetiredAt *time.Time             `json:"retired_at,omitempty"`
}

type diskReceipt struct {
	Version     int                     `json:"storage_version"`
	Fingerprint string                  `json:"fingerprint"`
	Body        *retainedbody.Reference `json:"body,omitempty"`
	Changed     bool                    `json:"changed"`
	Done        bool                    `json:"done"`
}

func legacyProcessHeader(path string) bool {
	raw, err := maintenance.Read(path, 128<<20)
	if err != nil {
		return false
	}
	var header struct {
		Version     int    `json:"storage_version"`
		ID          string `json:"id"`
		Fingerprint string `json:"fingerprint"`
	}
	return json.Unmarshal(raw, &header) == nil && header.Version == 0 && (validID(header.ID) || len(header.Fingerprint) == 64)
}

func metadataRecord(r Record) Record {
	r.Output, r.ValueApply, r.ValueWatch = nil, nil, nil
	if r.InputVersion != nil {
		// Only the public revision is needed by list consumers.
		copy := *r.InputVersion
		copy.Slots = nil
		r.InputVersion = &copy
	}
	return r
}

func writeRecord(path string, r Record) error {
	if _, err := readDiskRecord(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return writeRecordFormat(path, r)
}

func writeRecordFormat(path string, r Record) error {
	root := filepath.Dir(filepath.Dir(filepath.Dir(path)))
	unlock, err := (retainedbody.Store{Root: root}).Publication(context.Background())
	if err != nil {
		return err
	}
	defer unlock()
	body, err := json.Marshal(r)
	if err != nil {
		return err
	}
	ref, err := (retainedbody.Store{Root: root}).Put(context.Background(), "process_record", body)
	if err != nil {
		return err
	}
	if r.Capture {
		release, err := acquireLogLock(context.Background(), filepath.Join(filepath.Dir(path), "output.log"), true)
		if err != nil {
			return err
		}
		release()
	}
	return tasks.WritePrivate(path, diskRecord{Version: 2, Metadata: metadataRecord(r), Body: ref})
}

func readDiskRecord(path string) (diskRecord, error) {
	var d diskRecord
	err := tasks.ReadPrivate(path, &d)
	if err != nil {
		// Old records are decoded only by the explicit storage adapter.
		if legacyProcessHeader(path) {
			return d, storageupgrade.Required("processes")
		}
		return d, err
	}
	if d.Version != 2 || !d.Body.Valid() || d.Body.Kind != "process_record" {
		return d, errors.New("invalid process storage format")
	}
	return d, nil
}

func (s Store) recordAt(path string) (Record, error) {
	if _, err := readDiskRecord(path); err != nil {
		return Record{}, err
	}
	unlock, err := (retainedbody.Store{Root: s.Data}).Query(context.Background())
	if err != nil {
		return Record{}, err
	}
	defer unlock()
	d, err := readDiskRecord(path)
	if err != nil {
		return Record{}, err
	}
	body, err := (retainedbody.Store{Root: s.Data}).Read(d.Body)
	if err != nil {
		return Record{}, err
	}
	var r Record
	if err := json.Unmarshal(body, &r); err != nil {
		return r, err
	}
	if !validRecord(r.ID, r) {
		return r, errors.New("invalid process body")
	}
	r.DetailRetired = d.RetiredAt != nil
	return r, nil
}

// RecordBytes exposes the logical body to cleanup without copying the storage
// wrapper into an archive that must later restore the original record.
func (s Store) RecordBytes(id string) ([]byte, error) {
	if _, err := readDiskRecord(s.path(id, "record.json")); err != nil {
		return nil, err
	}
	unlock, err := (retainedbody.Store{Root: s.Data}).Query(context.Background())
	if err != nil {
		return nil, err
	}
	defer unlock()
	d, err := readDiskRecord(s.path(id, "record.json"))
	if err != nil {
		return nil, err
	}
	return (retainedbody.Store{Root: s.Data}).Read(d.Body)
}

func (s Store) RestoreRecord(r Record) error { return writePrivate(s.path(r.ID, "record.json"), r) }

func (s Store) RetireDetail(id string, now time.Time) error {
	path := s.path(id, "record.json")
	d, err := readDiskRecord(path)
	if err != nil {
		return err
	}
	if d.RetiredAt != nil {
		return nil
	}
	d.RetiredAt = &now
	return tasks.WritePrivate(path, d)
}

func writeReceipt(path string, r receipt) error {
	var existing diskReceipt
	if err := tasks.ReadPrivate(path, &existing); err == nil {
		if existing.Version != 2 {
			return errors.New("unsupported process receipt")
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return storageupgrade.Required("processes")
	}
	return writeReceiptFormat(path, r)
}

func writeReceiptFormat(path string, r receipt) error {
	data := filepath.Dir(filepath.Dir(filepath.Dir(path)))
	unlock, err := (retainedbody.Store{Root: data}).Publication(context.Background())
	if err != nil {
		return err
	}
	defer unlock()
	d := diskReceipt{Version: 2, Fingerprint: r.Fingerprint, Done: r.Done, Changed: r.Result.Changed}
	if r.Result.Item.ID != "" {
		body, err := json.Marshal(r.Result.Item)
		if err != nil {
			return err
		}
		root := filepath.Dir(filepath.Dir(filepath.Dir(path)))
		ref, err := (retainedbody.Store{Root: root}).Put(context.Background(), "process_record", body)
		if err != nil {
			return err
		}
		d.Body = &ref
	}
	return tasks.WritePrivate(path, d)
}

func (s Store) readReceipt(path string, r *receipt) error {
	if _, err := os.Lstat(path); err != nil {
		return err
	}
	var header diskReceipt
	if err := tasks.ReadPrivate(path, &header); err != nil {
		if legacyProcessHeader(path) {
			return storageupgrade.Required("processes")
		}
		return err
	}
	unlock, err := (retainedbody.Store{Root: s.Data}).Query(context.Background())
	if err != nil {
		return err
	}
	defer unlock()
	var d diskReceipt
	if err := tasks.ReadPrivate(path, &d); err != nil {
		if legacyProcessHeader(path) {
			return storageupgrade.Required("processes")
		}
		return err
	}
	if d.Version != 2 {
		return errors.New("invalid process receipt version")
	}
	*r = receipt{Fingerprint: d.Fingerprint, Done: d.Done, Result: Result{Changed: d.Changed}}
	if d.Body != nil {
		body, err := (retainedbody.Store{Root: s.Data}).Read(*d.Body)
		if err != nil {
			return err
		}
		if err = json.Unmarshal(body, &r.Result.Item); err != nil {
			return err
		}
	}
	return nil
}
