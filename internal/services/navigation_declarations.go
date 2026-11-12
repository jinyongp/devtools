package services

import (
	"path/filepath"
	"sort"

	"github.com/jinyongp/devtools/internal/location"
	"github.com/jinyongp/devtools/internal/ports"
	"github.com/jinyongp/devtools/internal/profilekey"
	"github.com/jinyongp/devtools/internal/protocol"
)

func (s Store) navigationDeclarations(q NavigationSelection, records []Record) ([]ServiceDeclaration, *protocol.Error) {
	directories := map[string]string{}
	for _, r := range records {
		if q.Profile != "" && q.Profile != r.Profile || q.Instance != "" && q.Instance != r.Instance || q.Directory != "" && !location.Same(q.Directory, r.Directory) {
			continue
		}
		directories[r.Directory] = r.Instance
	}
	state, e := (ports.Store{Directory: filepath.Join(s.Data, "ports")}).Read()
	if e != nil {
		return nil, e
	}
	for _, instance := range state.Instances {
		if q.Profile != "" && q.Profile != instance.Profile || q.Instance != "" && q.Instance != instance.ID || q.Directory != "" && !location.Same(q.Directory, instance.Directory) {
			continue
		}
		directories[instance.Directory] = instance.ID
	}
	if q.Directory != "" {
		if _, ok := directories[q.Directory]; !ok {
			directories[q.Directory] = ""
		}
	} else if q.Instance == "" {
		if p, e := profilekey.ResolveProject(s.Data, ".", ""); e == nil && (q.Profile == "" || p.Profile == q.Profile) {
			if _, ok := directories[p.Root]; !ok {
				directories[p.Root] = ""
			}
		}
	}
	ordered := []string{}
	for directory := range directories {
		ordered = append(ordered, directory)
	}
	sort.Strings(ordered)
	items := []ServiceDeclaration{}
	for _, directory := range ordered {
		p, e := profilekey.ResolveProject(s.Data, directory, "")
		// Retired/missing/invalid definitions do not erase observed running or
		// unresolved executions. Their metadata remains in the service summary.
		if e != nil || q.Profile != "" && p.Profile != q.Profile {
			continue
		}
		for name, c := range p.Commands {
			items = append(items, ServiceDeclaration{Profile: p.Profile, Directory: p.Root, Command: name, Env: c.Env, Instance: directories[directory], Served: len(c.Serve) > 0})
		}
	}
	return items, nil
}
