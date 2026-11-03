package retainedbody

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/jinyongp/devtools/internal/maintenance"
	"github.com/jinyongp/devtools/internal/storageupgrade"
)

type Envelope struct {
	Version int       `json:"storage_version"`
	Body    Reference `json:"body"`
}

func (s Store) WriteReference(ctx context.Context, path, kind string, body []byte) error {
	unlock, err := s.Publication(ctx)
	if err != nil {
		return err
	}
	defer unlock()
	ref, err := s.Put(ctx, kind, body)
	if err != nil {
		return err
	}
	raw, err := json.Marshal(Envelope{Version: 2, Body: ref})
	if err != nil {
		return err
	}
	return maintenance.Write(path, raw)
}

func (s Store) ReadReference(path string) ([]byte, error) {
	initial, err := maintenance.Read(path, MaxBytes)
	if err != nil {
		return nil, err
	}
	var header Envelope
	if json.Unmarshal(initial, &header) != nil || header.Version == 0 {
		return nil, storageupgrade.Required("archives")
	}
	unlock, err := s.Query(context.Background())
	if err != nil {
		return nil, err
	}
	defer unlock()
	raw, err := maintenance.Read(path, MaxBytes)
	if err != nil {
		return nil, err
	}
	var e Envelope
	if json.Unmarshal(raw, &e) != nil || e.Version == 0 {
		return nil, storageupgrade.Required("archives")
	}
	if e.Version != 2 || !e.Body.Valid() {
		return nil, errors.New("unsupported archive format")
	}
	return s.Read(e.Body)
}
