package proxy

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/jinyongp/devtools/internal/ports"
	"github.com/jinyongp/devtools/internal/protocol"
)

type pausedResolverContext struct {
	context.Context
	once    sync.Once
	entered chan struct{}
	resume  chan struct{}
}

func (c *pausedResolverContext) Err() error {
	c.once.Do(func() { close(c.entered); <-c.resume })
	return c.Context.Err()
}

func TestResolverConcurrentListsCannotLeaveStaleAggregate(t *testing.T) {
	store := ports.Store{Directory: filepath.Join(t.TempDir(), "ports")}
	directory := t.TempDir()
	writeConfig(t, directory, "app", "concurrent.localhost")
	alias := "main"
	instance := register(t, store, "app", directory, &alias, 30521)
	resolver := NewResolver(store)
	ctx := &pausedResolverContext{Context: context.Background(), entered: make(chan struct{}), resume: make(chan struct{})}
	first := make(chan *protocol.Error, 1)
	go func() { _, err := resolver.List(ctx, "app"); first <- err }()
	<-ctx.entered // First list has read the old registry snapshot.
	if err := store.Update(context.Background(), func(s *ports.State) (bool, *protocol.Error) {
		s.Get(instance.ID, "web").Port = 30522
		return true, nil
	}); err != nil {
		close(ctx.resume)
		t.Fatal(err)
	}
	second := make(chan *protocol.Error, 1)
	go func() { _, err := resolver.List(context.Background(), "app"); second <- err }()
	// A serialized resolver queues the second read; an overlapping resolver can
	// publish the new aggregate before the older read resumes.
	var secondErr *protocol.Error
	secondDone := false
	select {
	case secondErr = <-second:
		secondDone = true
	case <-time.After(time.Second):
	}
	close(ctx.resume)
	if err := <-first; err != nil {
		t.Fatal(err)
	}
	if !secondDone {
		secondErr = <-second
	}
	if secondErr != nil {
		t.Fatal(secondErr)
	}
	items, err := resolver.List(context.Background(), "app")
	if err != nil || len(items) != 1 || items[0].TargetPort == nil || *items[0].TargetPort != 30522 {
		t.Fatalf("older list permanently replaced the new route: %+v %v", items, err)
	}
}

func TestResolverQueuedListHonorsCancellation(t *testing.T) {
	store := ports.Store{Directory: filepath.Join(t.TempDir(), "ports")}
	directory := t.TempDir()
	writeConfig(t, directory, "app", "cancel.localhost")
	register(t, store, "app", directory, nil, 30524)
	resolver := NewResolver(store)
	firstContext := &pausedResolverContext{Context: context.Background(), entered: make(chan struct{}), resume: make(chan struct{})}
	first := make(chan struct{})
	go func() { resolver.List(firstContext, "app"); close(first) }()
	<-firstContext.entered
	ctx, cancel := context.WithCancel(context.Background())
	second := make(chan *protocol.Error, 1)
	go func() { _, err := resolver.List(ctx, "app"); second <- err }()
	cancel()
	select {
	case err := <-second:
		if err == nil || err.Code != "canceled" || err.ExitCode != 130 {
			t.Error(err)
		}
	case <-time.After(time.Second):
		t.Error("canceled list waited for another request")
	}
	close(firstContext.resume)
	<-first
}

func TestResolverCacheRejectsReplacedDirectorySymlink(t *testing.T) {
	store := ports.Store{Directory: filepath.Join(t.TempDir(), "ports")}
	parent := t.TempDir()
	directory := filepath.Join(parent, "project")
	if err := os.Mkdir(directory, 0700); err != nil {
		t.Fatal(err)
	}
	writeConfig(t, directory, "app", "identity.localhost")
	alias := "main"
	register(t, store, "app", directory, &alias, 30523)
	resolver := NewResolver(store)
	if items, err := resolver.List(context.Background(), "app"); err != nil || len(items) != 1 || items[0].Status != StatusReady {
		t.Fatal(items, err)
	}
	away := filepath.Join(parent, "away")
	if err := os.Rename(directory, away); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(away, directory); err != nil {
		t.Fatal(err)
	}
	for _, r := range []Resolver{resolver, {Ports: store}} {
		items, err := r.List(context.Background(), "app")
		if err != nil || len(items) != 1 || items[0].Status != StatusInstanceConflict || items[0].TargetPort != nil {
			t.Fatalf("changed canonical root kept a ready route: %+v %v", items, err)
		}
	}
	if err := os.Remove(directory); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(away, directory); err != nil {
		t.Fatal(err)
	}
	if items, err := resolver.List(context.Background(), "app"); err != nil || len(items) != 1 || items[0].Status != StatusReady {
		t.Fatal(items, err)
	}
}

func TestResolverProfileFilterPreservesGlobalHostConflict(t *testing.T) {
	store := ports.Store{Directory: filepath.Join(t.TempDir(), "ports")}
	firstDir, secondDir := t.TempDir(), t.TempDir()
	writeConfig(t, firstDir, "first", "shared.localhost")
	writeConfig(t, secondDir, "second", "shared.localhost")
	firstAlias, secondAlias := "first", "second"
	register(t, store, "first", firstDir, &firstAlias, 30401)
	register(t, store, "second", secondDir, &secondAlias, 30402)

	resolver := NewResolver(store)
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

func TestResolverCacheObservesConfigAndAssignmentChanges(t *testing.T) {
	store := ports.Store{Directory: filepath.Join(t.TempDir(), "ports")}
	directory := t.TempDir()
	writeConfig(t, directory, "app", "one.localhost")
	alias := "main"
	instance := register(t, store, "app", directory, &alias, 30501)
	resolver := NewResolver(store)

	items, err := resolver.List(context.Background(), "app")
	if err != nil || len(items) != 1 || items[0].Host == nil || *items[0].Host != "one.localhost" {
		t.Fatalf("initial route: %+v %v", items, err)
	}
	if resolver.cache == nil || len(resolver.cache.entries) != 1 {
		t.Fatalf("config was not cached: %#v", resolver.cache)
	}

	writeConfig(t, directory, "app", "changed-route.localhost")
	items, err = resolver.List(context.Background(), "app")
	if err != nil || len(items) != 1 || items[0].Host == nil || *items[0].Host != "changed-route.localhost" {
		t.Fatalf("config change was not observed: %+v %v", items, err)
	}
	if err := store.Update(context.Background(), func(state *ports.State) (bool, *protocol.Error) {
		state.Get(instance.ID, "web").Port = 30502
		return true, nil
	}); err != nil {
		t.Fatal(err)
	}
	items, err = resolver.List(context.Background(), "app")
	if err != nil || len(items) != 1 || items[0].TargetPort == nil || *items[0].TargetPort != 30502 {
		t.Fatalf("assignment change was not observed: %+v %v", items, err)
	}
}

func TestResolverCacheStillEnforcesPrivatePortDirectory(t *testing.T) {
	store := ports.Store{Directory: filepath.Join(t.TempDir(), "ports")}
	directory := t.TempDir()
	writeConfig(t, directory, "app", "private.localhost")
	alias := "main"
	register(t, store, "app", directory, &alias, 30511)
	resolver := NewResolver(store)

	if items, err := resolver.List(context.Background(), "app"); err != nil || len(items) != 1 {
		t.Fatalf("warm resolver: %+v %v", items, err)
	}
	if err := os.Chmod(store.Directory, 0755); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(store.Directory, 0700)
	if items, err := resolver.List(context.Background(), "app"); err == nil {
		t.Fatalf("cached registry bypassed private directory validation: %+v", items)
	}
}

func TestResolverCacheStillRejectsSymlinkedRegistry(t *testing.T) {
	store := ports.Store{Directory: filepath.Join(t.TempDir(), "ports")}
	directory := t.TempDir()
	writeConfig(t, directory, "app", "private.localhost")
	alias := "main"
	register(t, store, "app", directory, &alias, 30512)
	resolver := NewResolver(store)

	if items, err := resolver.List(context.Background(), "app"); err != nil || len(items) != 1 {
		t.Fatalf("warm resolver: %+v %v", items, err)
	}
	registry := filepath.Join(store.Directory, "registry.json")
	backing := filepath.Join(store.Directory, "registry.backing")
	if err := os.Rename(registry, backing); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("registry.backing", registry); err != nil {
		t.Fatal(err)
	}
	if items, err := resolver.List(context.Background(), "app"); err == nil {
		t.Fatalf("cached registry bypassed symlink rejection: %+v", items)
	}
}

func TestResolverCacheRecoversAfterLocationReturns(t *testing.T) {
	store := ports.Store{Directory: filepath.Join(t.TempDir(), "ports")}
	parent := t.TempDir()
	directory := filepath.Join(parent, "project")
	if err := os.Mkdir(directory, 0700); err != nil {
		t.Fatal(err)
	}
	writeConfig(t, directory, "app", "return.localhost")
	alias := "main"
	register(t, store, "app", directory, &alias, 30513)
	resolver := NewResolver(store)

	if items, err := resolver.List(context.Background(), "app"); err != nil || len(items) != 1 || items[0].Status != StatusReady {
		t.Fatalf("warm resolver: %+v %v", items, err)
	}
	away := filepath.Join(parent, "project-away")
	if err := os.Rename(directory, away); err != nil {
		t.Fatal(err)
	}
	items, err := resolver.List(context.Background(), "app")
	if err != nil || len(items) != 1 || items[0].Status != StatusLocationMissing {
		t.Fatalf("missing location not reported: %+v %v", items, err)
	}
	if err := os.Rename(away, directory); err != nil {
		t.Fatal(err)
	}
	items, err = resolver.List(context.Background(), "app")
	if err != nil || len(items) != 1 || items[0].Status != StatusReady {
		t.Fatalf("restored location stayed stale: %+v %v", items, err)
	}
}
