package expose

import (
	"context"
	"path/filepath"

	"github.com/jinyongp/devtools/internal/idref"
	"github.com/jinyongp/devtools/internal/protocol"
)

func (s Store) ResolveID(ctx context.Context, input string) (string, *protocol.Error) {
	if idref.UUID.Exact(input) {
		return input, nil
	}
	names, err := idref.DirectoryNames(filepath.Join(s.Data, "exposures"))
	if err != nil {
		return "", Failure("io_error")
	}
	return idref.Resolve(ctx, idref.UUID, input, idref.FileKeys(names, ".json"))
}
