package vault

import "context"

// PublishUpgradeHeld is restricted to the explicit storage conversion boundary.
// It keeps recovery encrypted even when the old live profile was plaintext.
// The caller holds launch mutation, key-slot and exclusive maintenance leases.
func (s FileStore) PublishUpgradeHeld(ctx context.Context, p *Profile, key *Material, id, fingerprint string) error {
	catalog, err := s.managedCatalog(p)
	if err != nil {
		return err
	}
	files, err := s.publicationFiles(ctx, catalog, p, key, nil)
	if err != nil {
		return err
	}
	_, err = (ForwardStore{Root: s.Root, VaultID: s.VaultID, Master: s.CatalogKey}).ReplaceHeld(ctx, id, fingerprint, files)
	return err
}
