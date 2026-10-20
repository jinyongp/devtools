package values

import (
	"bytes"
	"context"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/jinyongp/devtools/internal/protocol"
)

func TestPreparedLegacyWriteComparesStoredSource(t *testing.T) {
	for _, changed := range []bool{false, true} {
		for _, concurrent := range []bool{false, true} {
			t.Run(fmt.Sprintf("changed=%v/concurrent=%v", changed, concurrent), func(t *testing.T) {
				ctx := context.Background()
				s := ModernStore{Directory: filepath.Join(privateTempDir(t), "profiles"), Profile: "LegacyProfile"}
				if err := os.MkdirAll(s.Directory, 0700); err != nil {
					t.Fatal(err)
				}
				path := filepath.Join(s.Directory, hex.EncodeToString([]byte(s.Profile))+".json")
				source := []byte(`{"version":1,"profile":"LegacyProfile","envs":{},"keys":{"URL":{"kind":"variable","common":"before","envs":{}}}}`)
				if err := writePrivateFixture(path, source, 0600); err != nil {
					t.Fatal(err)
				}
				if concurrent {
					s.beforeCommit = func() {
						if err := os.WriteFile(path, bytes.ReplaceAll(source, []byte("before"), []byte("concurrent")), 0600); err != nil {
							t.Fatal(err)
						}
					}
				}
				actual, e := s.Update(ctx, func(editor *Editor) (bool, *protocol.Error) {
					value := "before"
					if changed {
						value = "after"
					}
					return editor.Set(Variable, "URL", "", value)
				})
				if concurrent {
					if e == nil || e.Code != "revision_conflict" || actual {
						t.Fatalf("concurrent source must win: changed=%v error=%v", actual, e)
					}
				} else if e != nil || actual != changed {
					t.Fatalf("unchanged source must be accepted: changed=%v error=%v", actual, e)
				}
				state, e := s.Read(ctx)
				if e != nil {
					t.Fatal(e)
				}
				value, _, e := state.GetVariable("URL", "")
				want := "before"
				if concurrent {
					want = "concurrent"
				} else if changed {
					want = "after"
				}
				if e != nil || value != want {
					t.Fatalf("stored value=%q want=%q error=%v", value, want, e)
				}
			})
		}
	}
}
