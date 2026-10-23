package values

import (
	"github.com/jinyongp/devtools/internal/vault"
	"maps"
	"time"
)

// Prerequisite is a value-free view of an authenticated manifest. It is kept
// separate from InputVersion so delivery authority and digests do not change.
type Prerequisite struct {
	Kind                       Kind
	Selected, HasSlot, Expired bool
}

func profilePrerequisites(p *vault.Profile, env, selection string, now time.Time) map[string]Prerequisite {
	selected := map[string]bool{}
	for _, b := range p.Bundles {
		if b.Name == selection {
			for _, id := range b.ValueIDs {
				selected[id] = true
			}
		}
	}
	out := map[string]Prerequisite{}
	for _, v := range p.Values {
		r := Prerequisite{Kind: Kind(v.Kind), Selected: selection == "" || selection == "all" || selected[v.ID]}
		var chosen *vault.Slot
		for i := range v.Slots {
			s := &v.Slots[i]
			if s.Env == env {
				chosen = s
				break
			}
			if s.Env == "" {
				chosen = s
			}
		}
		if chosen != nil {
			r.HasSlot = true
			r.Expired = chosen.ExpiresAt != nil && now.Unix() >= *chosen.ExpiresAt
		}
		out[v.Name] = r
	}
	return out
}

func (s *State) Prerequisites() map[string]Prerequisite {
	if s == nil {
		return nil
	}
	return maps.Clone(s.prerequisites)
}
