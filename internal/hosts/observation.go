package hosts

import (
	"encoding/json"
	"net/netip"
	"strings"
)

type Observation struct {
	committed map[ownerMapping]bool
	planned   map[ownerMapping]bool
	addresses map[string]netip.Addr
	conflicts map[string]bool
	issue     string
}

type ownerMapping struct {
	owner string
	value Mapping
}

func indexOwners(owners []Owner) map[ownerMapping]bool {
	out := make(map[ownerMapping]bool)
	for _, owner := range owners {
		for _, mapping := range owner.Mappings {
			out[ownerMapping{owner.Key, mapping}] = true
		}
	}
	return out
}

func stateDigest(st state) string {
	data, _ := json.Marshal(st)
	return digest(data)
}

// Observe reads one hosts image for all routes in this resolution. A journal
// transition during the read invalidates the pair, even if the bytes parse.
func (m Manager) Observe() Observation {
	for range 2 {
		st, e := m.read()
		if e != nil {
			break
		}
		if len(st.Owners) == 0 && st.Pending == nil {
			return Observation{}
		}
		body, busy, e := m.System.observe()
		if e != nil {
			break
		}
		after, e := m.read()
		if e != nil {
			break
		}
		if stateDigest(st) == stateDigest(after) {
			return observe(st, body, busy)
		}
	}
	return Observation{issue: "hosts_unavailable"}
}

func observe(st state, body []byte, busy bool) Observation {
	out := Observation{committed: indexOwners(st.Owners)}
	known := mappings(st.Owners)
	exact := false
	if p := st.Pending; p != nil {
		out.planned = indexOwners(p.Owners)
		hash := digest(body)
		switch hash {
		case p.After:
			known = mappings(p.Owners)
			exact = true
		case p.Request.Expected:
			exact = true
		default:
			known = append(known, mappings(p.Owners)...)
		}
	}
	if busy && !exact {
		out.issue = "hosts_unavailable"
		return out
	}
	if e := validateOwnership(body, known); e != nil {
		out.issue = e.Code
		return out
	}
	out.addresses = make(map[string]netip.Addr)
	out.conflicts = make(map[string]bool)
	for _, line := range strings.Split(string(body), "\n") {
		content, _, _ := strings.Cut(line, "#")
		fields := strings.Fields(content)
		if len(fields) < 2 {
			continue
		}
		address, _ := netip.ParseAddr(fields[0])
		address = address.Unmap()
		for _, name := range fields[1:] {
			host := strings.ToLower(name)
			if previous, present := out.addresses[host]; !address.IsValid() || present && previous != address {
				out.conflicts[host] = true
			}
			out.addresses[host] = address
		}
	}
	return out
}

// MappingIssue is a route observation, not permission to change the hosts file.
func (o Observation) MappingIssue(owner string, mapping Mapping) string {
	if o.issue != "" {
		return o.issue
	}
	key := ownerMapping{owner, mapping}
	if o.planned != nil && o.planned[key] != o.committed[key] {
		return "hosts_recovery_required"
	}
	host := strings.ToLower(mapping.Host)
	if o.conflicts[host] {
		return "hosts_conflict"
	}
	address, e := netip.ParseAddr(mapping.Address)
	if !o.committed[key] || e != nil || o.addresses[host] != address.Unmap() {
		return "hosts_mapping_missing"
	}
	return ""
}
