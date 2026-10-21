package values

import (
	"crypto/rand"
	"encoding/base64"
	"io"
	"time"

	"github.com/jinyongp/devtools/internal/protocol"
	"github.com/jinyongp/devtools/internal/vault"
)

func (e *Editor) generateSecret(name, env string, replace bool, reader io.Reader) (bool, *protocol.Error) {
	if !keyPattern.MatchString(name) {
		return false, protocol.NewError("invalid_argument", "Provide a valid secret key.", 2, nil)
	}
	if err := e.state.CheckEnv(env); err != nil {
		return false, err
	}
	if entry := e.state.Keys[name]; entry != nil {
		if entry.Kind != Secret {
			return false, failure("kind_conflict", "The key has a different kind in this profile.")
		}
		if e.hasExactSlot(name, env) {
			if !replace {
				return false, failure("value_exists", "The selected layer already has a value.")
			}
			if e.expiredExactSlot(name, env) {
				return false, failure("value_expired", "The selected layer has expired.")
			}
		}
	}
	if e.master == nil {
		return false, VaultError(e.ctx, vault.ErrKeyMissing)
	}
	if err := e.ctx.Err(); err != nil {
		return false, VaultError(e.ctx, err)
	}
	if reader == nil {
		reader = rand.Reader
	}
	var bytes [32]byte
	defer clear(bytes[:])
	if _, err := io.ReadFull(reader, bytes[:]); err != nil {
		return false, VaultError(e.ctx, err)
	}
	return e.Set(Secret, name, env, base64.RawURLEncoding.EncodeToString(bytes[:]))
}

func (e *Editor) expiredExactSlot(name, env string) bool {
	expiry, exists := e.state.expiries[name][env]
	now := time.Now
	if e.generateNow != nil {
		now = e.generateNow
	}
	return exists && now().Unix() >= expiry
}
