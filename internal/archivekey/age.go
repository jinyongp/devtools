// Package archivekey defines the age formats shared by backup and profile transfer.
package archivekey

import (
	"fmt"
	"strings"

	"filippo.io/age"
)

// Format is the age stanza type used to protect the archive's file key.
type Format string

const (
	Passphrase Format = "scrypt"
	X25519     Format = "X25519"
	Hybrid     Format = "mlkem768x25519"
)

type Recipient interface {
	age.Recipient
	fmt.Stringer
}

// IsHybridRecipient reports the format of a canonical recipient.
func IsHybridRecipient(text string) bool { return strings.HasPrefix(text, "age1pq1") }

func ParseRecipient(text string) (Recipient, error) {
	text = strings.TrimSpace(text)
	if IsHybridRecipient(text) {
		return age.ParseHybridRecipient(text)
	}
	return age.ParseX25519Recipient(text)
}

// ParseIdentity returns the matching recipient for safety backups written during import.
func ParseIdentity(text string) (age.Identity, Recipient, error) {
	text = strings.TrimSpace(text)
	if strings.HasPrefix(text, "AGE-SECRET-KEY-PQ-1") {
		key, err := age.ParseHybridIdentity(text)
		if err != nil {
			return nil, nil, err
		}
		return key, key.Recipient(), nil
	}
	key, err := age.ParseX25519Identity(text)
	if err != nil {
		return nil, nil, err
	}
	return key, key.Recipient(), nil
}
