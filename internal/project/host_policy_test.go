package project

import (
	"testing"

	"github.com/jinyongp/devtools/internal/network"
)

func TestHostPolicyConfigurationAndLegacyDigest(t *testing.T) {
	base := "profile='app'\n[ports.web]\nport=4400\n[proxies.app]\nhost='app.localhost'\nport='web'\n"
	legacy := struct{ Host, Port, Listener, DNS string }{"app.localhost", "web", "", ""}
	for _, policy := range []string{"", "host_policy='rewrite'\n", "host_policy='preserve'\n"} {
		doc, err := ParseDocument([]byte(base+policy), "devtools.toml", t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		p := doc.Context.Proxies["app"]
		if policy == "host_policy='preserve'\n" {
			if p.HostPolicy != network.HostPreserve || Digest(p) == Digest(legacy) {
				t.Fatal("preserve was lost from the identity", p)
			}
		} else if p.HostPolicy != "" || Digest(p) != Digest(legacy) {
			t.Fatal("default policy changed the legacy digest", p)
		}
	}
	for _, policy := range []string{"''", "'unknown'", "'${POLICY}'", "true", "1", "[]"} {
		if _, err := ParseDocument([]byte(base+"host_policy="+policy+"\n"), "devtools.toml", t.TempDir()); err == nil {
			t.Errorf("accepted host_policy=%s", policy)
		}
	}
}
