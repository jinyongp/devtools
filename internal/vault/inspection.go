package vault

import (
	"context"
	"errors"
	"path/filepath"

	"github.com/jinyongp/devtools/internal/maintenance"
)

// Inspection borrows existing authentication before disk gates. It never starts
// a session, unlocks protection, or extends its idle deadline.
type Inspection struct {
	Root     string
	Master   *Material
	VaultID  string
	identity [32]byte
}

func PrepareInspection(ctx context.Context, root string) (*Inspection, error) {
	s := KeyFileStore{Directory: filepath.Join(root, "vault")}
	slot, err := s.Read()
	if errors.Is(err, ErrKeyMissing) {
		return &Inspection{Root: root}, nil
	}
	if err != nil {
		return nil, err
	}
	identity, err := s.WriteIdentity()
	if err != nil {
		return nil, err
	}
	c, err := OpenSessionClient(s.Directory)
	if err != nil {
		return nil, err
	}
	master, err := c.InspectCached(ctx)
	if err != nil {
		return nil, err
	}
	return &Inspection{Root: root, Master: master, VaultID: slot.VaultID, identity: identity}, nil
}
func (i *Inspection) Close() {
	if i != nil {
		i.Master.Destroy()
	}
}

// Lease precedes maintenance; callers already hold their services gate.
func (i *Inspection) Lease(ctx context.Context) (func(), error) {
	if i.Master == nil {
		return func() {}, nil
	}
	return maintenance.LockShared(ctx, filepath.Join(i.Root, "vault", "key-slot-write.lock"))
}
func (i *Inspection) CheckHeld() error {
	if i.Master == nil {
		return CheckPublicRootHeld(i.Root)
	}
	identity, err := (KeyFileStore{Directory: filepath.Join(i.Root, "vault")}).WriteIdentity()
	if err != nil {
		return err
	}
	if identity != i.identity {
		return ErrInputChanged
	}
	return nil
}
