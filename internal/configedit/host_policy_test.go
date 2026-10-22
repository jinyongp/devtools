package configedit

import (
	"os"
	"testing"

	"github.com/jinyongp/devtools/internal/network"
	"github.com/jinyongp/devtools/internal/project"
)

func TestHostPolicyPatchAndRemoval(t *testing.T) {
	path, revision := fixture(t, "profile='app'\n[ports.web]\nport=4400\n[proxies.app]\nhost='app.localhost'\nport='web'\n")
	for _, value := range []any{"preserve", nil} {
		result := edit(t, path, revision, false, Operation{Op: "proxy.update", Name: "app", Value: map[string]any{"host_policy": value}})
		revision = result.Revision
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		doc, e := project.ParseDocument(data, path, "")
		if e != nil {
			t.Fatal(e)
		}
		want := network.HostRewrite
		if value != nil {
			want = network.HostPreserve
		}
		if got := doc.Context.Proxies["app"].HostPolicy.Effective(); got != want {
			t.Fatalf("policy = %s, want %s", got, want)
		}
	}
}
