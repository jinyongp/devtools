package vault

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/jinyongp/devtools/internal/platformfs"
	"golang.org/x/crypto/argon2"
)

// Measure the real production Argon2 preset separately from reusable-key work.
// This is an in-process key-session measurement, excluding CLI/IPC and native
// OS protection. Filesystem reads and explicit unlock's durable latch are real.
func BenchmarkPassphraseKeySession(b *testing.B) {
	for _, cold := range []bool{true, false} {
		name := "warm-use"
		if cold {
			name = "cold-explicit-unlock"
		}
		b.Run(name, func(b *testing.B) {
			base, err := filepath.EvalSymlinks(b.TempDir())
			if err != nil {
				b.Fatal(err)
			}
			directory := filepath.Join(base, "private", "vault")
			root, err := platformfs.OpenRoot(directory, true)
			if err != nil {
				b.Fatal(err)
			}
			root.Close()
			id, err := NewID()
			if err != nil {
				b.Fatal(err)
			}
			pass := NewMaterial([]byte("synthetic-session-benchmark-passphrase"))
			defer pass.Destroy()
			protector := PassphraseProtector{VaultID: id, Passphrase: pass}
			want, err := (KeyFileStore{Directory: directory}).Initialize(context.Background(), id, "passphrase", protector)
			if err != nil {
				b.Fatal(err)
			}
			defer want.Destroy()
			session := NewKeySession(directory)
			defer session.Close()
			calls := 0
			session.protector = func(slot KeyFile, input *Material, interactive bool) (Protector, error) {
				p := PassphraseProtector{VaultID: slot.VaultID, Passphrase: input}
				p.kdf = func(password, salt []byte, passes, memory uint32, lanes uint8, length uint32) []byte {
					calls++
					return argon2.IDKey(password, salt, passes, memory, lanes, length)
				}
				return p, nil
			}
			ctx := context.Background()
			if err := session.Unlock(ctx, pass, false); err != nil {
				b.Fatal(err)
			}
			calls = 0
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if cold {
					b.StopTimer()
					if err := session.Lock(ctx); err != nil {
						b.Fatal(err)
					}
					b.StartTimer()
					if err := session.Unlock(ctx, pass, false); err != nil {
						b.Fatal(err)
					}
				} else {
					key, err := session.Use(ctx)
					if err != nil {
						b.Fatal(err)
					}
					key.Destroy()
				}
			}
			b.StopTimer()
			key, err := session.Use(ctx)
			if err != nil {
				b.Fatal(err)
			}
			if !sameMaterial(key, want) {
				key.Destroy()
				b.Fatal("session result changed")
			}
			key.Destroy()
			expected := 0
			if cold {
				expected = b.N
			}
			if calls != expected {
				b.Fatal("unexpected KDF reuse count")
			}
			b.ReportMetric(float64(calls)/float64(b.N), "kdf-calls/op")
		})
	}
}
