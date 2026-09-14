package proxy

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/jinyongp/devtools/internal/ports"
	"github.com/jinyongp/devtools/internal/protocol"
)

func writeConfig(t *testing.T, directory, profile, host string) {
	t.Helper()
	content := "profile=\"" + profile + "\"\n[ports.web]\nrange=[30000,30999]\n[proxies.app]\nhost=\"" + host + "\"\nport=\"web\"\n"
	if err := os.WriteFile(filepath.Join(directory, "devtools.toml"), []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
}

func register(t *testing.T, store ports.Store, profile, directory string, alias *string, port int) ports.Instance {
	t.Helper()
	canonical, pathErr := filepath.EvalSymlinks(directory)
	if pathErr != nil {
		t.Fatal(pathErr)
	}
	directory = canonical
	var out ports.Instance
	err := store.Update(context.Background(), func(state *ports.State) (bool, *protocol.Error) {
		instance, err := state.Register(profile, directory)
		if err != nil {
			return false, err
		}
		instance.Alias = alias
		state.SyncInstance(*instance)
		out = *instance
		if port != 0 {
			state.Assignments = append(state.Assignments, ports.Assignment{Instance: *instance, Name: "web", Port: port})
		}
		return true, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestResolverListsInstancesAndCurrentAssignments(t *testing.T) {
	store := ports.Store{Directory: filepath.Join(t.TempDir(), "ports")}
	mainDir, featureDir := t.TempDir(), t.TempDir()
	writeConfig(t, mainDir, "app", "${instance.alias}.app.localhost")
	writeConfig(t, featureDir, "app", "${instance.alias}.app.localhost")
	main, feature := "main", "feature"
	mainInstance := register(t, store, "app", mainDir, &main, 30101)
	register(t, store, "app", featureDir, &feature, 30102)

	resolver := Resolver{Ports: store}
	items, err := resolver.List(context.Background(), "app")
	if err != nil || len(items) != 2 || *items[0].Host != "main.app.localhost" || *items[0].TargetPort != 30101 || items[1].Status != StatusReady {
		t.Fatalf("initial routes: %+v %v", items, err)
	}
	err = store.Update(context.Background(), func(state *ports.State) (bool, *protocol.Error) {
		state.Get(mainInstance.ID, "web").Port = 30103
		return true, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	items, err = resolver.List(context.Background(), "app")
	if err != nil || *items[0].TargetPort != 30103 {
		t.Fatalf("updated route: %+v %v", items, err)
	}
}

func TestResolverReportsRouteStatesAndConflicts(t *testing.T) {
	store := ports.Store{Directory: filepath.Join(t.TempDir(), "ports")}
	missingAlias, missingAssignment, firstConflict, secondConflict := t.TempDir(), t.TempDir(), t.TempDir(), t.TempDir()
	writeConfig(t, missingAlias, "app", "${instance.alias}.app.localhost")
	writeConfig(t, missingAssignment, "app", "idle.app.localhost")
	writeConfig(t, firstConflict, "app", "same.app.localhost")
	writeConfig(t, secondConflict, "app", "same.app.localhost")
	register(t, store, "app", missingAlias, nil, 30201)
	idle := "idle"
	register(t, store, "app", missingAssignment, &idle, 0)
	first, second := "first", "second"
	register(t, store, "app", firstConflict, &first, 30202)
	register(t, store, "app", secondConflict, &second, 30203)

	items, err := (Resolver{Ports: store}).List(context.Background(), "")
	if err != nil || len(items) != 4 {
		t.Fatalf("items: %+v %v", items, err)
	}
	statuses := map[string]int{}
	for _, item := range items {
		statuses[item.Status]++
	}
	if statuses[StatusAliasMissing] != 1 || statuses[StatusAssignmentMissing] != 1 || statuses[StatusHostConflict] != 2 {
		t.Fatalf("statuses: %+v", statuses)
	}
}

func TestResolverRejectsStaleAndUnavailableInstances(t *testing.T) {
	store := ports.Store{Directory: filepath.Join(t.TempDir(), "ports")}
	changedProfile := t.TempDir()
	writeConfig(t, changedProfile, "app", "app.localhost")
	alias := "changed"
	register(t, store, "app", changedProfile, &alias, 30301)
	writeConfig(t, changedProfile, "other", "app.localhost")

	parent := t.TempDir()
	writeConfig(t, parent, "app", "parent.app.localhost")
	child := filepath.Join(parent, "child")
	if err := os.Mkdir(child, 0700); err != nil {
		t.Fatal(err)
	}
	childAlias := "child"
	register(t, store, "app", child, &childAlias, 30302)

	invalid := t.TempDir()
	if err := os.WriteFile(filepath.Join(invalid, "devtools.toml"), []byte("broken=["), 0600); err != nil {
		t.Fatal(err)
	}
	invalidAlias := "invalid"
	register(t, store, "app", invalid, &invalidAlias, 30303)

	removed := t.TempDir()
	removedAlias := "removed"
	register(t, store, "app", removed, &removedAlias, 30304)
	if err := os.Remove(removed); err != nil {
		t.Fatal(err)
	}

	items, err := (Resolver{Ports: store}).List(context.Background(), "app")
	if err != nil || len(items) != 4 {
		t.Fatalf("items: %+v %v", items, err)
	}
	statuses := map[string]int{}
	for _, item := range items {
		if item.Kind != "instance" || item.Host != nil || item.Proxy != nil || item.Service != nil || item.TargetPort != nil {
			t.Fatalf("invented route data: %+v", item)
		}
		statuses[item.Status]++
	}
	if statuses[StatusInstanceConflict] != 2 || statuses[StatusConfigInvalid] != 1 || statuses[StatusLocationMissing] != 1 {
		t.Fatalf("statuses: %+v", statuses)
	}
}
