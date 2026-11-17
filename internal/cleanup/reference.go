package cleanup

import (
	"context"
	"slices"

	"github.com/jinyongp/devtools/internal/idref"
	"github.com/jinyongp/devtools/internal/protocol"
)

func resolveCleanupID(ctx context.Context, input, directory, suffix string) (string, *protocol.Error) {
	if idref.UUID.Exact(input) {
		return input, nil
	}
	names, err := idref.DirectoryNames(directory)
	if err != nil {
		return "", fail("storage_error", err)
	}
	return idref.Resolve(ctx, idref.UUID, input, idref.FileKeys(names, suffix))
}

func resolveCleanupItems(ctx context.Context, inputs, keys []string) ([]string, *protocol.Error) {
	resolved, condition := idref.ResolveMany(ctx, idref.UUID, inputs, slices.Values(keys))
	if condition != nil {
		return nil, condition
	}
	slices.Sort(resolved)
	for i := 1; i < len(resolved); i++ {
		if resolved[i-1] == resolved[i] {
			return nil, fail("invalid_argument")
		}
	}
	return resolved, nil
}
