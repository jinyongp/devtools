package values

import (
	"crypto/sha256"
	"encoding/hex"
	"sort"

	"github.com/jinyongp/devtools/internal/vault"
)

// InputSpec describes only profile contributions. Parent environment policy,
// code/config changes and external service authentication are separate facts.
type InputSpec struct {
	Selection string   `json:"selection"`
	Inject    bool     `json:"inject"`
	Bindings  []string `json:"bindings"`
	Shadowed  []string `json:"shadowed"`
}

func (s InputSpec) UsesValues() bool { return s.Inject || len(s.Bindings) > 0 }

// InputVersion is value-free, with no key, endpoint, session or launch ticket.
// A nil version denotes an older/unknown delivery, distinct from a known empty
// selection whose Slots is an empty array.
type InputVersion struct {
	Identity          vault.InputIdentity   `json:"identity"`
	Spec              InputSpec             `json:"spec"`
	Env               string                `json:"env"`
	Slots             []vault.SlotReference `json:"slots"`
	IntegrityVerified bool                  `json:"integrity_verified"`
	KeyEpoch          uint64                `json:"key_epoch,omitempty"`
}

func (v *InputVersion) Clone() *InputVersion {
	if v == nil {
		return nil
	}
	c := *v
	c.Spec.Bindings = append([]string{}, v.Spec.Bindings...)
	c.Spec.Shadowed = append([]string{}, v.Spec.Shadowed...)
	c.Slots = append([]vault.SlotReference{}, v.Slots...)
	return &c
}
func normalizedNames(names []string) []string {
	set := map[string]bool{}
	for _, n := range names {
		set[n] = true
	}
	out := make([]string, 0, len(set))
	for n := range set {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}
func NewInputVersion(source SnapshotMetadata, env string, spec InputSpec) (*InputVersion, error) {
	if spec.Selection == "" {
		spec.Selection = "all"
	}
	if !vault.ValidSelection(spec.Selection) {
		return nil, vault.ErrInvalid
	}
	spec.Bindings = normalizedNames(spec.Bindings)
	spec.Shadowed = normalizedNames(spec.Shadowed)
	for _, names := range [][]string{spec.Bindings, spec.Shadowed} {
		for _, name := range names {
			if !keyPattern.MatchString(name) {
				return nil, vault.ErrInvalid
			}
		}
	}
	out := &InputVersion{Spec: spec, Env: env, Slots: []vault.SlotReference{}}
	if spec.UsesValues() {
		// Legacy fixture/provider snapshots have no stable slot identities. Do not
		// manufacture a known delivery from their plaintext State copy.
		if (source.Identity.ProfileID == "" || source.Identity.StorageGeneration == "") && source.Public == nil {
			return nil, nil
		}
		out.Identity = source.Identity
		out.KeyEpoch = source.KeyEpoch
		out.IntegrityVerified = source.IntegrityVerified || source.Launch != nil
		bound, shadowed := map[string]bool{}, map[string]bool{}
		for _, name := range spec.Bindings {
			bound[name] = true
		}
		for _, name := range spec.Shadowed {
			shadowed[name] = true
		}
		for _, slot := range source.Slots {
			if bound[slot.Name] || spec.Inject && !shadowed[slot.Name] {
				out.Slots = append(out.Slots, slot)
			}
		}
	}
	sort.Slice(out.Slots, func(i, j int) bool { return out.Slots[i].Name < out.Slots[j].Name })
	encoded := vault.NewEncoder("delivered-profile-input")
	encoded.String(out.Identity.ProfileID)
	encoded.String(out.Identity.StorageGeneration)
	for _, slot := range out.Slots {
		encoded.String(slot.Name)
		encoded.String(slot.Env)
		encoded.String(slot.ValueID)
		encoded.String(slot.SlotID)
		encoded.String(slot.Version)
	}
	bytes, err := encoded.Finish()
	if err != nil {
		return nil, err
	}
	digest := sha256.Sum256(bytes)
	out.Identity.Revision = hex.EncodeToString(digest[:])
	return out, nil
}

// SameInput ignores key re-encryption, display metadata, unused slot revisions
// and selector spelling. Incarnation and actual effective slot versions count.
func SameInput(left, right *InputVersion) (bool, bool) {
	if left == nil || right == nil {
		return false, false
	}
	return left.Identity == right.Identity, true
}
