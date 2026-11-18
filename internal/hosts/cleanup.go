package hosts

import (
	"bytes"
	"sort"

	"github.com/jinyongp/devtools/internal/privilege"
	"github.com/jinyongp/devtools/internal/protocol"
)

// CleanupStep is private confirmation for one ordered owner removal.
type CleanupStep struct {
	Owner string
	Token string
}

func (m Manager) PreviewCleanup(selected func(string) bool) ([]CleanupStep, *protocol.Error) {
	st, e := m.read()
	if e != nil {
		return nil, e
	}
	keys := map[string]bool{}
	collect := func(owners []Owner) {
		for _, owner := range owners {
			if selected(owner.Key) {
				keys[owner.Key] = true
			}
		}
	}
	collect(st.Owners)
	if st.Pending != nil {
		collect(st.Pending.Owners)
	}
	steps := []CleanupStep{}
	if len(keys) == 0 {
		return steps, nil
	}
	if st.Pending != nil {
		return nil, recoveryFailure("pending_hosts_operation", st.Pending)
	}
	body, busy, e := m.System.observe()
	if e != nil {
		return nil, e
	}
	if busy {
		return nil, recoveryFailure("outcome_unconfirmed", nil)
	}
	current, e := m.read()
	if e != nil {
		return nil, e
	}
	if stateDigest(st) != stateDigest(current) {
		return nil, failure("stale_preview")
	}
	if e := validateOwnership(body, mappings(st.Owners)); e != nil {
		return nil, e
	}
	owners := make([]string, 0, len(keys))
	for key := range keys {
		owners = append(owners, key)
	}
	sort.Strings(owners)
	for _, owner := range owners {
		steps = append(steps, CleanupStep{owner, m.previewToken(st, body, owner, nil)})
		st.Owners = replaceOwner(st.Owners, owner, nil)
		after, e := Render(body, mappings(st.Owners))
		if e != nil {
			return nil, e
		}
		if !bytes.Equal(body, after) {
			if m.Isolated {
				return nil, isolatedWriteFailure()
			}
			if e := privilege.CheckRequest("hosts", Request{Expected: digest(body), Mappings: mappings(st.Owners)}); e != nil {
				return nil, e
			}
		}
		body = after
	}
	return steps, nil
}
