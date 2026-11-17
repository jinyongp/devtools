package ports

import (
	"context"
	"strings"

	"github.com/jinyongp/devtools/internal/idref"
	"github.com/jinyongp/devtools/internal/location"
	"github.com/jinyongp/devtools/internal/protocol"
)

func (st *State) ResolveInstanceSelector(ctx context.Context, profile, directory, selector string) (string, *protocol.Error) {
	input, marked := strings.CutPrefix(selector, "id:")
	if !marked || idref.Hex.Exact(input) {
		return selector, nil
	}
	id, condition := idref.Resolve(ctx, idref.Hex, input, func(yield func(string) bool) {
		for _, instance := range st.Instances {
			if (profile == "" || instance.Profile == profile) &&
				(directory == "" || location.Same(directory, instance.Directory)) && !yield(instance.ID) {
				return
			}
		}
	})
	if condition != nil {
		return "", condition
	}
	return "id:" + id, nil
}
