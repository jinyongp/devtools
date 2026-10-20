package values

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jinyongp/devtools/internal/protocol"
	"github.com/jinyongp/devtools/internal/vault"
)

// Benchmark the real encrypted overwrite path, including file leases and durable
// profile/catalog/hint publication. Initial key protection and seeding are excluded.
func BenchmarkModernBatchOverwrite(b *testing.B) {
	for _, size := range []int{100, 1000, 5000} {
		b.Run(fmt.Sprint(size), func(b *testing.B) {
			ctx := context.Background()
			root := b.TempDir()
			if err := os.Chmod(root, 0700); err != nil {
				b.Fatal(err)
			}
			id, _ := vault.NewID()
			pass := vault.NewMaterial([]byte("synthetic-benchmark-protection"))
			defer pass.Destroy()
			master, err := (vault.KeyFileStore{Directory: filepath.Join(root, "vault")}).Initialize(ctx, id, "passphrase", vault.PassphraseProtector{VaultID: id, Passphrase: pass})
			if err != nil {
				b.Fatal(err)
			}
			defer master.Destroy()
			if err = (vault.FileStore{Root: root, VaultID: id, CatalogKey: master}).Initialize(ctx); err != nil {
				b.Fatal(err)
			}
			s := ModernStore{Directory: filepath.Join(root, "profiles"), Profile: "bench", Master: func(context.Context) (*vault.Material, error) {
				raw := master.CopyBytes()
				defer clear(raw)
				return vault.NewMaterial(raw), nil
			}}
			inputs := [2]map[string]string{{}, {}}
			for i := range size {
				for arm := range 2 {
					inputs[arm][fmt.Sprintf("TOKEN_%04d", i)] = fmt.Sprintf("synthetic-benchmark-%d-%04d", arm, i)
				}
			}
			if _, e := s.Update(ctx, func(e *Editor) (bool, *protocol.Error) {
				_, changed, err := e.Import(inputs[1], nil, "", true)
				return changed, err
			}); e != nil {
				b.Fatal(e)
			}
			phases := map[string]time.Duration{}
			s.observe = func(phase string, d time.Duration) { phases[phase] += d }
			b.ReportAllocs()
			b.ResetTimer()
			for n := 0; n < b.N; n++ {
				if _, e := s.Update(ctx, func(e *Editor) (bool, *protocol.Error) {
					_, changed, err := e.Import(inputs[n%2], nil, "", true)
					return changed, err
				}); e != nil {
					b.Fatal(e)
				}
			}
			b.StopTimer()
			for _, phase := range []string{"prepare", "read-wait", "read-held", "commit-wait", "commit-held"} {
				if phases[phase] <= 0 {
					b.Fatalf("missing phase measurement: %s", phase)
				}
			}
			for phase, total := range phases {
				b.ReportMetric(float64(total.Nanoseconds())/float64(b.N), phase+"-ns/op")
			}
		})
	}
}
