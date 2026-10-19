package values

import (
	"github.com/jinyongp/devtools/internal/vault"
	"sort"
)

func mutateBundle(p *vault.Profile, key *vault.Material, c Change) (bool, error) {
	b := c.Bundle
	if b == nil || !vault.ValidBundleName(b.Name) || c.Import != nil || c.Key != "" || c.Value != nil || c.Env != "" {
		return false, vault.ErrInvalid
	}
	switch c.Action {
	case "bundle.set":
		if b.NewName != "" {
			return false, vault.ErrInvalid
		}
		return p.SetBundle(b.Name, b.Members, key)
	case "bundle.rename":
		if len(b.Members) != 0 {
			return false, vault.ErrInvalid
		}
		return p.RenameBundle(b.Name, b.NewName, key)
	case "bundle.remove":
		if len(b.Members) != 0 || b.NewName != "" {
			return false, vault.ErrInvalid
		}
		return p.RemoveBundle(b.Name, key)
	default:
		return false, vault.ErrInvalid
	}
}
func importBundle(p *vault.Profile, key *vault.Material, name string, items []ImportItem) (bool, error) {
	selected := map[string]bool{}
	for _, b := range p.Bundles {
		if b.Name == name {
			for _, id := range b.ValueIDs {
				for _, v := range p.Values {
					if v.ID == id {
						selected[v.Name] = true
					}
				}
			}
		}
	}
	for _, item := range items {
		selected[item.Key] = true
	}
	members := make([]string, 0, len(selected))
	for name := range selected {
		members = append(members, name)
	}
	sort.Strings(members)
	return p.SetBundle(name, members, key)
}
