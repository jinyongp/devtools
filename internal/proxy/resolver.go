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

	"github.com/jinyongp/devtools/internal/hosts"
	"github.com/jinyongp/devtools/internal/network"
	"github.com/jinyongp/devtools/internal/platformfs"
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
	Listener   string  `json:"listener,omitempty"`
	Scope      string  `json:"scope,omitempty"`
	RouteID    string  `json:"route_id,omitempty"`
}

func (i Item) Ref() network.RouteRef {
	if i.Scope == "global" {
		return network.RouteRef{Scope: "global", RouteID: i.RouteID}
	}
	name := ""
	if i.Proxy != nil {
		name = *i.Proxy
	}
	return network.RouteRef{Scope: "project", Profile: i.Profile, InstanceID: i.InstanceID, Proxy: name}
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
	info, err = platformfs.StatPrivate(path)
	if err != nil {
		return fileIdentity{}, err
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
	routing  *routingSnapshot
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
	if directoryErr != nil || !platformfs.DirectoryPrivate(r.Ports.Directory, directory) {
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
	filtered := make([]Item, 0, len(items))
	for _, item := range items {
		if profile == "" || item.Profile == profile {
			item.Host = copyRoutePointer(item.Host)
			item.Proxy = copyRoutePointer(item.Proxy)
			item.Alias = copyRoutePointer(item.Alias)
			item.Service = copyRoutePointer(item.Service)
			item.TargetPort = copyRoutePointer(item.TargetPort)
			filtered = append(filtered, item)
		}
	}
	return filtered
}

func copyRoutePointer[T any](value *T) *T {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
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

// routeSnapshot collects identity inputs from the same listing. It belongs to
// one call; tracked sources must never be reused across requests.
type routeSnapshot struct {
	routingOnly bool
	routing     *routingSnapshot
	ref         network.RouteRef
	state       *ports.State
	source      string
	sourceErr   *protocol.Error
}

func (r Resolver) List(ctx context.Context, profile string) ([]Item, *protocol.Error) {
	return r.list(ctx, profile, nil)
}

func (r Resolver) list(ctx context.Context, profile string, snapshot *routeSnapshot) ([]Item, *protocol.Error) {
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
	if snapshot != nil {
		snapshot.state = state
	}
	type resolution struct {
		instance ports.Instance
		status   string
		config   project.Context
		err      *protocol.Error
	}
	resolved := make([]resolution, 0, len(state.Instances))
	stable := r.cache != nil && !registryChanged && (snapshot == nil || snapshot.routingOnly)
	if state.Network != nil {
		for _, a := range state.Network.Activations {
			if a.Mode == "active" {
				stable = false
				break
			}
		}
	}
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
		cached := r.cache.items
		routing := r.cache.routing
		ok := r.cache.itemsOK
		r.cache.mu.Unlock()
		if ok {
			if snapshot != nil && snapshot.routingOnly {
				snapshot.routing = routing
				return nil, nil
			}
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
		var source string
		var sourceErr *protocol.Error
		sourceChecked := false
		if snapshot != nil && snapshot.ref.Scope == "project" && snapshot.ref.Profile == instance.Profile && snapshot.ref.InstanceID == instance.ID {
			source, sourceErr = config.NetworkSources()
			sourceChecked = true
			snapshot.source, snapshot.sourceErr = source, sourceErr
		}
		for _, name := range names {
			item := route(*state, instance, name, config.Proxies[name])
			if definition := config.Ports[config.Proxies[name].Port]; definition.Expression != nil && item.Status == StatusReady && state.Activation(item.Ref()) == nil {
				item.Status = "activation_required"
			}
			if a := state.Activation(item.Ref()); a != nil && a.Mode == "active" && a.EnvironmentBacked {
				if !sourceChecked {
					source, sourceErr = config.NetworkSources()
					sourceChecked = true
				}
				if sourceErr != nil || source != a.SourceDigest {
					item.Status = "activation_required"
				}
			}
			if item.Status == StatusReady {
				r.dnsStatus(state, &item, config.Proxies[name].DNS)
			}
			items = append(items, item)
		}
	}
	if state.Network != nil {
		for _, definition := range state.Network.Routes {
			name, host := definition.Name, definition.Host
			item := Item{Kind: "route", Scope: "global", RouteID: definition.ID, Proxy: &name, Host: &host, Listener: definition.Listener, Status: "inactive"}
			if reservation := state.Reservation(definition.Reservation); reservation != nil {
				port := reservation.Port
				item.TargetPort = &port
			}
			if a := state.Activation(definition.Ref()); a != nil && a.Mode == "active" && item.TargetPort != nil {
				item.Status = StatusReady
				r.dnsStatus(state, &item, definition.DNS)
			}
			items = append(items, item)
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
	routing := newRoutingSnapshot(items)
	if snapshot != nil {
		snapshot.routing = routing
	}
	if r.cache != nil {
		r.cache.mu.Lock()
		r.cache.routing = routing
		r.cache.items = items
		r.cache.itemsOK = true
		r.cache.mu.Unlock()
	}
	if snapshot != nil && snapshot.routingOnly {
		return nil, nil
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
	item.Listener = network.ListenerName(definition.Listener)
	activation := state.Activation(item.Ref())
	disabled := activation != nil && activation.Mode == "disabled"
	if disabled && project.NetworkEnvironmentBacked(definition.Host) {
		item.Status = "inactive"
		return item
	}
	if p := state.Get(instance.ID, definition.Port); p != nil {
		port := p.Port
		item.TargetPort = &port
	}
	backed := project.NetworkEnvironmentBacked(definition.Host)
	if activation != nil && activation.Mode == "active" && activation.EnvironmentBacked {
		host := activation.Host
		item.Host = &host
		item.Listener = activation.Listener
		if project.Digest(definition) != activation.ConfigDigest {
			item.Status = "activation_required"
		} else if item.TargetPort == nil {
			item.Status = StatusAssignmentMissing
		} else {
			item.Status = StatusReady
		}
		return item
	}
	// Environment expressions are interpreted by the initiating CLI only.
	if backed {
		item.Status = "activation_required"
		return item
	}
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
	if err != nil || !network.ValidHost(host) {
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
	if disabled {
		item.Status = "inactive"
		return item
	}
	if activation == nil && (!project.ValidProxyHost(host) || item.Listener != network.DefaultListener || definition.DNS != "" && definition.DNS != "auto") {
		item.Status = "activation_required"
	}
	return item
}

func (r Resolver) dnsStatus(state *ports.State, item *Item, dns string) {
	if dns == "" {
		dns = "auto"
	}
	listener := state.Listener(item.Listener)
	if listener == nil {
		if item.Listener != network.DefaultListener {
			item.Status = "listener_not_found"
		}
		return
	}
	if item.Host == nil {
		return
	}
	desired, e := DNSMapping(*item.Host, dns, *listener)
	if e != nil {
		item.Status = "activation_required"
		return
	}
	if len(desired) > 0 && !(hosts.Manager{Data: filepath.Dir(r.Ports.Directory)}).Prepared(item.Ref().Key()+"/"+listener.ID, desired[0]) {
		item.Status = "activation_required"
	}
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
