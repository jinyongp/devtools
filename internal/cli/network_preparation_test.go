package cli

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/jinyongp/devtools/internal/execution"
	"github.com/jinyongp/devtools/internal/network"
	"github.com/jinyongp/devtools/internal/ports"
	"github.com/jinyongp/devtools/internal/project"
	"github.com/jinyongp/devtools/internal/protocol"
	"github.com/jinyongp/devtools/internal/tasks"
)

func TestAutomaticPreparationSkipsDisabledBeforeEnvironmentAndRetainsPeerPorts(t *testing.T) {
	root := privateTempDir(t)
	config := "profile='app'\n[ports.web]\n[ports.other]\nport='${UNUSED_NETWORK_PORT}'\n[proxies.app]\nhost='${MISSING_NETWORK_DOMAIN}'\nport='web'\n[proxies.unrelated]\nhost='${UNUSED_NETWORK_DOMAIN}'\nport='other'\n[commands.web]\nexec=" + fixtureExec(t, "true") + "\nserve=['web']\n[commands.worker]\nexec=" + fixtureExec(t, "true") + "\n"
	if e := os.WriteFile(filepath.Join(root, "devtools.toml"), []byte(config), 0600); e != nil {
		t.Fatal(e)
	}
	p, e := project.Resolve(root, "")
	if e != nil {
		t.Fatal(e)
	}
	app := testApp(t)
	manager, e := app.proxyStore()
	if e != nil {
		t.Fatal(e)
	}
	ps := managerPortStore(manager)
	var ref network.RouteRef
	if e := ps.Update(context.Background(), func(st *ports.State) (bool, *protocol.Error) {
		i, e := st.Register(p.Profile, p.Root)
		if e != nil {
			return false, e
		}
		ref = network.RouteRef{Scope: "project", Profile: p.Profile, InstanceID: i.ID, Proxy: "app"}
		return true, st.SetActivation(network.Activation{Route: ref, Mode: "disabled"})
	}); e != nil {
		t.Fatal(e)
	}
	snapshots, e := app.prepareNetwork(context.Background(), p, []string{"web", "worker"}, nil, nil, IO{}, tasks.ID(), network.PrepareAuto, false)
	if e != nil || len(snapshots) != 2 {
		t.Fatal(snapshots, e)
	}
	snapshot := snapshots["web"]
	if len(snapshot.Activations) != 1 || snapshot.Activations[0].Mode != "disabled" || len(snapshot.Listeners) != 0 || snapshot.Ports["web"] == 0 {
		t.Fatal(snapshot)
	}
	command, e := execution.ResolveConfigured(p, "web", nil, nil)
	if e != nil {
		t.Fatal(e)
	}
	if _, e := app.applyNetworkSnapshot(command, snapshot); e != nil {
		t.Fatal(e)
	}
	if _, e := app.prepareNetwork(context.Background(), p, []string{"web"}, nil, nil, IO{}, tasks.ID(), network.PrepareOn, false); e == nil || e.Code != "network_environment_required" {
		t.Fatal(e)
	}
	if e := ps.Update(context.Background(), func(st *ports.State) (bool, *protocol.Error) {
		st.Network.Activations = []network.Activation{}
		return true, nil
	}); e != nil {
		t.Fatal(e)
	}
	if _, e := app.applyNetworkSnapshot(command, snapshot); e == nil || e.Code != "network_snapshot_changed" {
		t.Fatal(e)
	}
}
