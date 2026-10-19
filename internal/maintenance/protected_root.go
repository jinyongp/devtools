package maintenance

import (
	"encoding/hex"
	"encoding/json"
	"errors"
)

func validVaultID(id string) bool {
	if len(id) != 32 {
		return false
	}
	_, err := hex.DecodeString(id)
	return err == nil
}

// ProtectedRootHeld inspects format state under an already-held maintenance gate.
// This marker blocks older binaries; it is not a substitute for authentication.
func ProtectedRootHeld(root string) (string, bool, error) {
	p, legacy, body, err := readPointer(root)
	if err != nil {
		return "", false, err
	}
	if body == nil {
		return "", false, nil
	}
	if !legacy && p.Version == 3 && p.Phase == "ready" {
		return p.VaultID, true, nil
	}
	return "", false, errors.New("restore recovery pending")
}

func writeProtectedRoot(root, vaultID string) error {
	if !validVaultID(vaultID) {
		return errors.New("invalid protected root")
	}
	b, err := json.Marshal(transactionPointer{Version: 3, Phase: "ready", VaultID: vaultID})
	if err != nil {
		return err
	}
	return Write(pendingPath(root), b)
}
