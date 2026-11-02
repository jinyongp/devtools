package values

import (
	"github.com/jinyongp/devtools/internal/vault"
)

// decodeCurrentProfile classifies old headers without decoding old value bodies.
func decodeCurrentProfile(raw []byte, profile string) (*vault.Profile, error) {
	p, err := vault.DecodeProfile(raw)
	if err != nil {
		return nil, err
	}
	if p.Name != profile {
		return nil, vault.ErrIntegrity
	}
	return p, nil
}
