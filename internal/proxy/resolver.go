// Package proxy resolves project proxy declarations and serves local routes.
package proxy

import (
	"context"
	"errors"
	"os"
	"sort"
	"strings"

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

type Resolver struct{ Ports ports.Store }

func (r Resolver) List(ctx context.Context, profile string) ([]Item, *protocol.Error) {
	if profile != "" && !project.ValidProfile(profile) {
		return nil, protocol.NewError("invalid_argument", "Invalid profile identifier.", 2, map[string]any{"field": "profile"})
	}
	state, err := r.Ports.Read()
	if err != nil {
		return nil, err
	}
	items := []Item{}
	for _, instance := range state.Instances {
		if profile != "" && instance.Profile != profile {
			continue
		}
		if status := locationStatus(instance.Directory); status != "" {
			items = append(items, diagnostic(instance, status))
			continue
		}
		config, configErr := project.Resolve(instance.Directory, "")
		if configErr != nil {
			status := StatusConfigUnavailable
			if configErr.Code == "invalid_config" || configErr.Code == "project_not_found" {
				status = StatusConfigInvalid
			}
			items = append(items, diagnostic(instance, status))
			continue
		}
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
			definition := config.Proxies[name]
			items = append(items, route(*state, instance, name, definition))
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
	return items, nil
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
