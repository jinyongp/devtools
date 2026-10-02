// Package proxy resolves project proxy declarations and serves local routes.
package proxy

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/jinyongp/devtools/internal/ports"
	"github.com/jinyongp/devtools/internal/project"
	"github.com/jinyongp/devtools/internal/protocol"
)

const (
	StatusReady             = "ready"
	StatusAliasMissing      = "alias_missing"
	StatusAssignmentMissing = "assignment_missing"
	StatusHostInvalid       = "host_invalid"
	StatusHostConflict      = "host_conflict"
	StatusLocationMissing   = "location_missing"
	StatusConfigInvalid     = "config_invalid"
	StatusConfigUnavailable = "config_unavailable"
	StatusInstanceConflict  = "instance_conflict"
)

type Item struct {
	Kind       string  `json:"kind"`
	Host       *string `json:"host"`
	Proxy      *string `json:"proxy"`
	Profile    string  `json:"profile"`
	InstanceID string  `json:"instance_id"`
	Alias      *string `json:"alias"`
	Directory  string  `json:"directory"`
	Service    *string `json:"service"`
	TargetPort *int    `json:"target_port"`
	Status     string  `json:"status"`
}

type fileIdentity struct {
	exists   bool
	modified int64
	size     int64
	mode     os.FileMode
	info     os.FileInfo
}

func statIdentity(path string) (fileIdentity, error) {
	info, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		return fileIdentity{}, nil
	}
	if err != nil {
		return fileIdentity{}, err
	}
	return fileIdentity{exists: true, modified: info.ModTime().UnixNano(), size: info.Size(), mode: info.Mode(), info: info}, nil
}

func privateRegistryIdentity(path string) (fileIdentity, error) {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return fileIdentity{}, nil
	}
	if err != nil {
		return fileIdentity{}, err
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 {
		return fileIdentity{}, errors.New("private regular file required")
	}
	return fileIdentity{exists: true, modified: info.ModTime().UnixNano(), size: info.Size(), mode: info.Mode(), info: info}, nil
}

func (identity fileIdentity) same(other fileIdentity) bool {
	if identity.exists != other.exists {
		return false
	}
	if !identity.exists {
		return true
	}
	return identity.modified == other.modified &&
		identity.size == other.size &&
		identity.mode == other.mode &&
		identity.info != nil &&
		other.info != nil &&
		os.SameFile(identity.info, other.info)
}

type cachedProject struct {
	identity           fileIdentity
	canonicalDirectory string
	config             project.Context
	err                *protocol.Error
}

type cachedRegistry struct {
	identity fileIdentity
	state    *ports.State
	valid    bool
}

type resolverCache struct {
	listGate chan struct{}
	mu       sync.Mutex
	entries  map[string]cachedProject
	registry cachedRegistry
	items    []Item
	itemsOK  bool
}

type Resolver struct {
	Ports ports.Store
	cache *resolverCache
}

func NewResolver(store ports.Store) Resolver {
	return Resolver{Ports: store, cache: &resolverCache{entries: map[string]cachedProject{}, listGate: make(chan struct{}, 1)}}
}

func (r Resolver) readPorts() (*ports.State, bool, *protocol.Error) {
	if r.cache == nil {
		state, err := r.Ports.Read()
		return state, true, err
	}
	directory, directoryErr := os.Lstat(r.Ports.Directory)
	if directoryErr != nil || !directory.IsDir() || directory.Mode().Perm()&0077 != 0 {
		state, err := r.Ports.Read()
		return state, true, err
	}
	identity, statErr := privateRegistryIdentity(filepath.Join(r.Ports.Directory, "registry.json"))
	if statErr != nil {
		state, err := r.Ports.Read()
		return state, true, err
	}
	r.cache.mu.Lock()
	cached := r.cache.registry
	r.cache.mu.Unlock()
	if cached.valid && cached.identity.same(identity) {
		return cached.state, false, nil
	}
	state, err := r.Ports.Read()
	if err != nil {
		return nil, true, err
	}
	r.cache.mu.Lock()
	r.cache.registry = cachedRegistry{identity: identity, state: state, valid: true}
	r.cache.mu.Unlock()
	return state, true, nil
}

func filterRouteItems(items []Item, profile string) []Item {
	if profile == "" {
		return append([]Item{}, items...)
	}
	filtered := make([]Item, 0)
	for _, item := range items {
		if item.Profile == profile {
			filtered = append(filtered, item)
		}
	}
	return filtered
}

func (r Resolver) resolveProject(instance ports.Instance) (project.Context, *protocol.Error, bool) {
	if r.cache == nil {
		config, err := project.Resolve(instance.Directory, "")
		return config, err, true
	}
	path := filepath.Join(instance.Directory, project.Filename)
	identity, statErr := statIdentity(path)
	if statErr != nil || !identity.exists || !identity.mode.IsRegular() {
		r.cache.mu.Lock()
		_, existed := r.cache.entries[path]
		delete(r.cache.entries, path)
		r.cache.mu.Unlock()
		config, err := project.Resolve(instance.Directory, "")
		return config, err, existed || statErr != nil || !identity.exists
	}
	r.cache.mu.Lock()
	cached, ok := r.cache.entries[path]
	r.cache.mu.Unlock()
	canonicalDirectory, canonicalErr := filepath.EvalSymlinks(instance.Directory)
	if ok && canonicalErr == nil && cached.canonicalDirectory == canonicalDirectory && cached.identity.same(identity) {
		return cached.config, cached.err, false
	}
	config, err := project.Resolve(instance.Directory, "")
	r.cache.mu.Lock()
	r.cache.entries[path] = cachedProject{identity: identity, canonicalDirectory: canonicalDirectory, config: config, err: err}
	r.cache.mu.Unlock()
	return config, err, true
}

func (r Resolver) List(ctx context.Context, profile string) ([]Item, *protocol.Error) {
	if profile != "" && !project.ValidProfile(profile) {
		return nil, protocol.NewError("invalid_argument", "Invalid profile identifier.", 2, map[string]any{"field": "profile"})
	}
	if r.cache != nil {
		// Registry, project identities and their aggregate must advance together.
		select {
		case <-ctx.Done():
			return nil, protocol.NewError("canceled", "Request canceled.", 130, nil)
		case r.cache.listGate <- struct{}{}:
		}
		defer func() { <-r.cache.listGate }()
	}
	state, registryChanged, err := r.readPorts()
	if err != nil {
		return nil, err
	}
	type resolution struct {
		instance ports.Instance
		status   string
		config   project.Context
		err      *protocol.Error
	}
	resolved := make([]resolution, 0, len(state.Instances))
	stable := r.cache != nil && !registryChanged
	for _, instance := range state.Instances {
		if ctx.Err() != nil {
			return nil, protocol.NewError("canceled", "Request canceled.", 130, nil)
		}
		if status := locationStatus(instance.Directory); status != "" {
			if r.cache != nil {
				path := filepath.Join(instance.Directory, project.Filename)
				r.cache.mu.Lock()
				delete(r.cache.entries, path)
				r.cache.mu.Unlock()
			}
			stable = false
			resolved = append(resolved, resolution{instance: instance, status: status})
			continue
		}
		config, configErr, changed := r.resolveProject(instance)
		if changed {
			stable = false
		}
		resolved = append(resolved, resolution{instance: instance, config: config, err: configErr})
	}
	if stable {
		r.cache.mu.Lock()
		cached := append([]Item(nil), r.cache.items...)
		ok := r.cache.itemsOK
		r.cache.mu.Unlock()
		if ok {
			return filterRouteItems(cached, profile), nil
		}
	}

	items := []Item{}
	for _, current := range resolved {
		instance := current.instance
		if current.status != "" {
			items = append(items, diagnostic(instance, current.status))
			continue
		}
		if current.err != nil {
			status := StatusConfigUnavailable
			if current.err.Code == "invalid_config" || current.err.Code == "project_not_found" {
				status = StatusConfigInvalid
			}
			items = append(items, diagnostic(instance, status))
			continue
		}
		config := current.config
		if config.Root != instance.Directory || config.Profile != instance.Profile {
			items = append(items, diagnostic(instance, StatusInstanceConflict))
			continue
		}
		names := make([]string, 0, len(config.Proxies))
		for name := range config.Proxies {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			items = append(items, route(*state, instance, name, config.Proxies[name]))
		}
	}
	conflicts := map[string]int{}
	for _, item := range items {
		if item.Kind == "route" && item.Host != nil {
			conflicts[*item.Host]++
		}
	}
	for index := range items {
		if items[index].Host != nil && conflicts[*items[index].Host] > 1 {
			items[index].Status = StatusHostConflict
		}
	}
	sort.Slice(items, func(left, right int) bool {
		a, b := items[left], items[right]
		return itemKey(a) < itemKey(b)
	})
	if r.cache != nil {
		r.cache.mu.Lock()
		r.cache.items = append([]Item(nil), items...)
		r.cache.itemsOK = true
		r.cache.mu.Unlock()
	}
	return filterRouteItems(items, profile), nil
}

func locationStatus(directory string) string {
	info, err := os.Stat(directory)
	if errors.Is(err, os.ErrNotExist) || err == nil && !info.IsDir() {
		return StatusLocationMissing
	}
	if err != nil {
		return StatusConfigUnavailable
	}
	return ""
}

func diagnostic(instance ports.Instance, status string) Item {
	return Item{Kind: "instance", Profile: instance.Profile, InstanceID: instance.ID, Alias: instance.Alias, Directory: instance.Directory, Status: status}
}

func route(state ports.State, instance ports.Instance, name string, definition project.Proxy) Item {
	service := definition.Port
	item := Item{Kind: "route", Proxy: &name, Profile: instance.Profile, InstanceID: instance.ID, Alias: instance.Alias, Directory: instance.Directory, Service: &service}
	if instance.Alias == nil && strings.Contains(definition.Host, "${instance.alias}") {
		item.Status = StatusAliasMissing
		return item
	}
	alias := ""
	if instance.Alias != nil {
		alias = *instance.Alias
	}
	host, err := project.Expand(definition.Host, func(key string) (string, bool) {
		switch key {
		case "profile":
			return instance.Profile, true
		case "instance.alias":
			return alias, true
		case "proxy":
			return name, true
		default:
			return "", false
		}
	})
	if err != nil || !project.ValidProxyHost(host) {
		item.Status = StatusHostInvalid
		return item
	}
	item.Host = &host
	assignment := state.Get(instance.ID, definition.Port)
	if assignment == nil {
		item.Status = StatusAssignmentMissing
		return item
	}
	port := assignment.Port
	item.TargetPort = &port
	item.Status = StatusReady
	return item
}

func itemKey(item Item) string {
	host, name := "", ""
	if item.Host != nil {
		host = *item.Host
	}
	if item.Proxy != nil {
		name = *item.Proxy
	}
	return item.Profile + "\x00" + item.Directory + "\x00" + item.Kind + "\x00" + host + "\x00" + name
}
