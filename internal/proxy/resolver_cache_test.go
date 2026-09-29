package proxy

import (
	"context"
	"testing"

	"github.com/jinyongp/devtools/internal/ports"
)

func TestResolverProfileFilterPreservesGlobalHostConflict(t *testing.T) {
	store := ports.Store{Directory: t.TempDir()}
	firstDir, secondDir := t.TempDir(), t.TempDir()
	writeConfig(t, firstDir, "first", "shared.localhost")
	writeConfig(t, secondDir, "second", "shared.localhost")
	firstAlias, secondAlias := "first", "second"
	register(t, store, "first", firstDir, &firstAlias, 30401)
	register(t, store, "second", secondDir, &secondAlias, 30402)

	resolver := Resolver{Ports: store}
	all, err := resolver.List(context.Background(), "")
	if err != nil || len(all) != 2 || all[0].Status != StatusHostConflict || all[1].Status != StatusHostConflict {
		t.Fatalf("global conflicts: %+v %v", all, err)
	}
	for _, profile := range []string{"first", "second"} {
		items, err := resolver.List(context.Background(), profile)
		if err != nil || len(items) != 1 || items[0].Profile != profile || items[0].Status != StatusHostConflict {
			t.Fatalf("filtered conflict %s: %+v %v", profile, items, err)
		}
	}
}
