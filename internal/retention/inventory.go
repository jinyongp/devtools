package retention

import "sort"

type Bucket struct {
	Kind            string `json:"kind"`
	Lifecycle       string `json:"lifecycle"`
	Scope           string `json:"scope"`
	Verification    string `json:"verification"`
	CleanupEligible bool   `json:"cleanup_eligible"`
	Reason          string `json:"reason"`
	Files           int    `json:"files"`
	LogicalItems    int    `json:"logical_items"`
	Bytes           int64  `json:"bytes"`
	seen            map[string]bool
}
type Inventory struct {
	Verification string   `json:"verification"`
	Profile      string   `json:"profile,omitempty"`
	Buckets      []Bucket `json:"buckets,omitempty"`
}

func Pending(profile string) Inventory {
	return Inventory{Verification: "verification_pending", Profile: profile}
}
func (i *Inventory) Add(kind, lifecycle, scope, verification, reason, id string, eligible bool, size int64) {
	for n := range i.Buckets {
		b := &i.Buckets[n]
		if b.Kind == kind && b.Lifecycle == lifecycle && b.Scope == scope && b.Verification == verification && b.Reason == reason && b.CleanupEligible == eligible {
			b.Files++
			b.Bytes += size
			if !b.seen[id] {
				b.LogicalItems++
				b.seen[id] = true
			}
			return
		}
	}
	i.Buckets = append(i.Buckets, Bucket{Kind: kind, Lifecycle: lifecycle, Scope: scope, Verification: verification, CleanupEligible: eligible, Reason: reason, Files: 1, LogicalItems: 1, Bytes: size, seen: map[string]bool{id: true}})
}
func (i *Inventory) Complete() {
	i.Verification = "verified"
	sort.Slice(i.Buckets, func(a, b int) bool {
		x, y := i.Buckets[a], i.Buckets[b]
		if x.Kind != y.Kind {
			return x.Kind < y.Kind
		}
		if x.Lifecycle != y.Lifecycle {
			return x.Lifecycle < y.Lifecycle
		}
		if x.Scope != y.Scope {
			return x.Scope < y.Scope
		}
		return x.Verification < y.Verification
	})
}
