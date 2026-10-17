package execution

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/jinyongp/devtools/internal/ports"
	"github.com/jinyongp/devtools/internal/project"
	"github.com/jinyongp/devtools/internal/protocol"
)

func TestWindowsCaseCollisionPreventsSpawn(t *testing.T) {
	p := Prepared{Injected: map[string]string{"Path": "private-one", "PATH": "private-two"}, Args: []string{"unused"}}
	called := false
	runner := func(context.Context, []string, string, []string, io.Reader, io.Writer, io.Writer) (int, *protocol.Error) {
		called = true
		return 0, nil
	}
	_, e := p.Execute(context.Background(), os.Environ(), runner, nil, io.Discard, io.Discard)
	if e == nil || e.Code != "invalid_argument" || called {
		t.Fatal("case-colliding injection reached runner")
	}
}
func TestWindowsCaseCollisionPreventsPortPreparation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "registry")
	command := Command{Bind: map[string]project.Binding{"Path": {}, "PATH": {}}}
	_, e := prepareResolved(context.Background(), command, nil, Dependencies{Ports: ports.Store{Directory: path}}, false, true)
	if e == nil || e.Code != "invalid_argument" {
		t.Fatalf("case collision was not diagnosed before port preparation: %v", e)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("case collision created registry storage")
	}
}
