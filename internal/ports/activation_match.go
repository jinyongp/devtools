package ports

import (
	"github.com/jinyongp/devtools/internal/network"
	"github.com/jinyongp/devtools/internal/project"
)

// ActivationMatches permits an unchanged pinned legacy implicit route to remain
// implicit. Writing an overlay solely for preparation would mutate its pin.
func (st *State) ActivationMatches(expected network.Activation) bool {
	if current := st.Activation(expected.Route); current != nil {
		return *current == expected
	}
	if expected.Mode != "active" || expected.EnvironmentBacked || expected.Listener != network.DefaultListener || expected.DNS != "auto" || !project.ValidProxyHost(expected.Host) || st.Network == nil {
		return false
	}
	for _, pin := range st.Network.Pins {
		if pin.Route == expected.Route && pin.NetworkDigest == expected.NetworkDigest {
			return true
		}
	}
	return false
}
