package cli

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jinyongp/devtools/internal/execution"
	"github.com/jinyongp/devtools/internal/lifecycle"
	"github.com/jinyongp/devtools/internal/maintenance"
	"github.com/jinyongp/devtools/internal/network"
	"github.com/jinyongp/devtools/internal/ports"
	"github.com/jinyongp/devtools/internal/profilekey"
	"github.com/jinyongp/devtools/internal/project"
	"github.com/jinyongp/devtools/internal/protocol"
	proxyapi "github.com/jinyongp/devtools/internal/proxy"
	"github.com/jinyongp/devtools/internal/services"
	"github.com/jinyongp/devtools/internal/tasks"
)

func TestRetiredProfileCannotCreateExecutionOrPreparationReceipts(t *testing.T) {
	projectDirectory := privateTempDir(t)
	t.Chdir(projectDirectory)
	t.Setenv("TOKEN", "test-marker")
	config := "profile='old'\n[commands.worker]\ninject=false\nexec=" + fixtureExec(t, "check-marker") + "\n"
	if err := os.WriteFile("devtools.toml", []byte(config), 0600); err != nil {
		t.Fatal(err)
	}
	app := testApp(t)
	profiles, failure := app.dataDirectory()
	if failure != nil {
		t.Fatal(failure)
	}
	root := filepath.Dir(profiles)
	owner := strings.Repeat("a", 32)
	names := profilekey.RenameNames{Version: 1, Owners: map[string]string{"old": owner, "new": owner}, Current: map[string]string{owner: "new"}}
	body, err := names.Encode()
	if err != nil {
		t.Fatal(err)
	}
	release, err := maintenance.AcquireExclusive(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	err = maintenance.Replace(root, map[string][]byte{profilekey.RenameNamesPath: body})
	release()
	if err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"run", "worker"},
		{"process", "start", "worker", "--request-id", tasks.ID()},
		{"project", "up", "worker", "--request-id", tasks.ID()},
	} {
		code, out, diagnostic := invoke(t, app, "", args...)
		if code != 3 || out != "" || !strings.Contains(diagnostic, `"code":"profile_renamed"`) {
			t.Fatalf("%v used a retired profile: %d %q %s", args, code, out, diagnostic)
		}
	}
	if _, err := os.Stat(filepath.Join(projectDirectory, "marker")); !os.IsNotExist(err) {
		t.Fatalf("a child of a retired profile ran: %v", err)
	}
	for _, directory := range []string{"processes", "project-operations", "network-operations"} {
		err := filepath.WalkDir(filepath.Join(root, directory), func(path string, entry fs.DirEntry, err error) error {
			if err == nil && !entry.IsDir() && !strings.HasSuffix(entry.Name(), ".lock") && !strings.HasSuffix(entry.Name(), ".lock.turnstile") {
				t.Errorf("retired name left an execution artifact: %s", path)
			}
			return err
		})
		if err != nil && !os.IsNotExist(err) {
			t.Fatal(err)
		}
	}
}

func TestForegroundUsesVersionedProcessDirectory(t *testing.T) {
	projectDirectory := privateTempDir(t)
	t.Chdir(projectDirectory)
	config := "profile='current'\n[commands.worker]\ninject=false\nexec=" + fixtureExec(t, "plain") + "\n"
	if err := os.WriteFile("devtools.toml", []byte(config), 0600); err != nil {
		t.Fatal(err)
	}
	app := testApp(t)
	profiles, failure := app.dataDirectory()
	if failure != nil {
		t.Fatal(failure)
	}
	root := filepath.Dir(profiles)
	legacy := filepath.Join(root, "processes")
	ctx := context.Background()
	release, err := maintenance.AcquireExclusive(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	if err := maintenance.FenceLegacyRecoveryHeld(root); err != nil {
		release()
		t.Fatal(err)
	}
	err = maintenance.Write(legacy, []byte(maintenance.RuntimeFenceDocument))
	release()
	if err != nil {
		t.Fatal(err)
	}
	if code, output, diagnostic := invoke(t, app, "", "run", "worker"); code != 0 || output != "plain" || diagnostic != "" {
		t.Fatalf("foreground run did not use the versioned layout: %d %q %s", code, output, diagnostic)
	}
	store := services.Store{Data: root}
	records, condition := store.StoredRecords()
	if condition != nil || len(records) != 1 || records[0].Profile != "current" || records[0].EndedAt == nil {
		t.Fatalf("versioned record missing or incomplete: %+v %v", records, condition)
	}
	if body, err := maintenance.Read(legacy, 1024); err != nil || string(body) != maintenance.RuntimeFenceDocument {
		t.Fatal("foreground run reopened the legacy path", err)
	}
}

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

func TestProjectAutoPreparationUsesWholeFreshUnionAndFreezesMode(t *testing.T) {
	root := privateTempDir(t)
	t.Chdir(root)
	config := "profile='app'\n[ports.web]\n[ports.db]\n[proxies.front]\nhost='batch.localhost'\nport='web'\n[commands.web]\nexec=" + fixtureExec(t, "true") + "\nserve=['web']\n[commands.web.bind]\nDB_PORT={port='db'}\n[commands.db]\nexec=" + fixtureExec(t, "true") + "\nserve=['db']\n[commands.worker]\nexec=" + fixtureExec(t, "true") + "\n[commands.worker.bind]\nAPP_URL={port='web',format='url'}\nDB_PORT={port='db'}\n"
	if e := os.WriteFile("devtools.toml", []byte(config), 0600); e != nil {
		t.Fatal(e)
	}
	app, _ := preparationTestApp(t)
	processes := &cliLifecycleProcesses{}
	app.lifecycleManager = func(data string) lifecycle.Manager { return lifecycle.Manager{Data: data, Processes: processes} }
	args := []string{"project", "up", "worker", "web", "db", "--request-id", tasks.ID()}
	result := outputData(t, app, args)
	if result["changed"] != true || len(processes.calls) != 3 {
		t.Fatal(result, processes.calls)
	}
	var dbPort string
	for _, call := range processes.calls {
		if !call.ProxyMode || call.Network == nil || len(call.Network.Ports) != 2 {
			t.Fatalf("fresh peer missing the union: %#v", call)
		}
		if call.Command == "web" {
			dbPort = fmt.Sprint(call.Network.Ports["db"])
			if call.Network.Bindings["DB_PORT"] != dbPort {
				t.Fatal(call.Network)
			}
		}
	}
	worker := processes.calls[0].Network
	if worker.Bindings["DB_PORT"] != dbPort || worker.Bindings["APP_URL"] == "" {
		t.Fatal(worker)
	}
	outputData(t, app, args)
	if len(processes.calls) != 3 {
		t.Fatal("completed replay relaunched children")
	}
	if code, _, diagnostic := invoke(t, app, "", append(args, "--no-proxy")...); code != 3 || !strings.Contains(diagnostic, "request_conflict") {
		t.Fatal(code, diagnostic)
	}
	if code, _, diagnostic := invoke(t, app, "", "project", "up", "db", "--proxy", "--request-id", tasks.ID()); code != 3 || !strings.Contains(diagnostic, "proxy_route_required") {
		t.Fatal(code, diagnostic)
	}
	data, _ := app.dataDirectory()
	for _, call := range processes.calls {
		path := networkSnapshotPath(filepath.Dir(data), call.RequestID)
		if e := tasks.PrivateDir(filepath.Dir(path)); e != nil {
			t.Fatal(e)
		}
		if e := tasks.WritePrivate(path, call.Network); e != nil {
			t.Fatal(e)
		}
	}
	reused := outputData(t, app, []string{"project", "up", "worker", "web", "db", "--request-id", tasks.ID()})
	if reused["changed"] != false || len(processes.records) != 3 {
		t.Fatal("active batch was replaced", reused)
	}
}

func TestProjectAutoPreparationPreservesExistingOffMember(t *testing.T) {
	root := privateTempDir(t)
	t.Chdir(root)
	config := "profile='app'\n[ports.web]\n[ports.api]\n[proxies.web]\nhost='old.localhost'\nport='web'\n[proxies.api]\nhost='new.localhost'\nport='api'\n[commands.web]\nexec=" + fixtureExec(t, "true") + "\nserve=['web']\n[commands.api]\nexec=" + fixtureExec(t, "true") + "\nserve=['api']\n"
	if e := os.WriteFile("devtools.toml", []byte(config), 0600); e != nil {
		t.Fatal(e)
	}
	p, e := project.Resolve(root, "")
	if e != nil {
		t.Fatal(e)
	}
	now := time.Now().UTC()
	original := services.Record{ID: tasks.ID(), Profile: p.Profile, Directory: p.Root, Command: "web", State: "running", CreatedAt: now, StartedAt: &now}
	app, m := preparationTestApp(t)
	processes := &cliLifecycleProcesses{records: []services.Record{original}}
	app.lifecycleManager = func(data string) lifecycle.Manager { return lifecycle.Manager{Data: data, Processes: processes} }
	outputData(t, app, []string{"project", "up", "web", "api", "--request-id", tasks.ID()})
	if len(processes.calls) != 2 || processes.calls[0].ProxyMode || !processes.calls[1].ProxyMode || processes.records[0].ID != original.ID {
		t.Fatal(processes.calls)
	}
	st, e := managerPortStore(m).Read()
	if e != nil {
		t.Fatal(e)
	}
	instance := st.Find(p.Profile, "", p.Root)
	for _, activation := range st.Network.Activations {
		if activation.Route.InstanceID == instance.ID && activation.Route.Proxy == "web" {
			t.Fatal("existing off member was activated")
		}
	}
}

func preparationTestApp(t *testing.T) (*App, proxyapi.Manager) {
	t.Helper()
	app := testApp(t)
	dir, _ := app.dataDirectory()
	m := proxyapi.Manager{Data: filepath.Dir(dir)}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	started := false
	m.Spawn = func(_ string, id string) error {
		started = true
		go func() { defer close(done); _ = m.Serve(ctx, id) }()
		return nil
	}
	t.Cleanup(func() {
		cancel()
		if started {
			select {
			case <-done:
			case <-time.After(5 * time.Second):
				t.Error("proxy fixture did not stop")
			}
		}
	})
	listener, e := net.Listen("tcp4", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	_ = listener.Close()
	if _, e := m.Configure(ctx, "default", fmt.Sprintf("127.0.0.1:%d", port), "", false, tasks.ID()); e != nil {
		t.Fatal(e)
	}
	app.proxyManager = func(string) proxyapi.Manager { return m }
	return app, m
}

func TestNamedRunAutomaticallyPreparesAndHonorsDisabledAndOverrides(t *testing.T) {
	root := privateTempDir(t)
	t.Chdir(root)
	config := "profile='app'\n[ports.web]\n[proxies.route]\nhost='auto.localhost'\nport='web'\n[commands.serve]\nserve=['web']\nexec=" + fixtureExec(t, "true") + "\n"
	if e := os.WriteFile("devtools.toml", []byte(config), 0600); e != nil {
		t.Fatal(e)
	}
	app, m := preparationTestApp(t)
	for _, flags := range [][]string{{"--no-proxy"}, {"--proxy=false"}} {
		args := append([]string{"run"}, flags...)
		args = append(args, "serve")
		if code, out, err := invoke(t, app, "", args...); code != 0 {
			t.Fatal(code, out, err)
		}
	}
	status, e := m.NetworkStatus(context.Background(), "default")
	if e != nil || status["running"] == true {
		t.Fatal(status, e)
	}
	if code, _, err := invoke(t, app, "", "run", "--no-proxy", "--proxy", "serve"); code != 2 {
		t.Fatal(code, err)
	}
	if code, out, err := invoke(t, app, "", "run", "serve"); code != 0 {
		t.Fatal(code, out, err)
	}
	status, e = m.NetworkStatus(context.Background(), "default")
	if e != nil || status["running"] != true {
		t.Fatal(status, e)
	}
	p, e := project.Resolve(root, "")
	if e != nil {
		t.Fatal(e)
	}
	st, e := managerPortStore(m).Read()
	if e != nil {
		t.Fatal(e)
	}
	i := st.Find(p.Profile, "", p.Root)
	ref := network.RouteRef{Scope: "project", Profile: p.Profile, InstanceID: i.ID, Proxy: "route"}
	hm, e := app.hostsManager(context.Background(), IO{})
	if e != nil {
		t.Fatal(e)
	}
	if _, e := m.RouteDown(context.Background(), ref, hm, tasks.ID()); e != nil {
		t.Fatal(e)
	}
	if code, _, err := invoke(t, app, "", "run", "serve"); code != 0 {
		t.Fatal(code, err)
	}
	st, _ = managerPortStore(m).Read()
	if st.Activation(ref).Mode != "disabled" {
		t.Fatal("automatic run revived disabled route")
	}
	if code, _, err := invoke(t, app, "", "run", "--proxy", "serve"); code != 0 {
		t.Fatal(code, err)
	}
	st, _ = managerPortStore(m).Read()
	if st.Activation(ref).Mode != "active" {
		t.Fatal("forced run did not activate route")
	}
}

func TestManagedPreparationFreezesModeAndReplaysLegacyWithoutPreparation(t *testing.T) {
	root := privateTempDir(t)
	config := "profile='app'\n[ports.web]\n[proxies.route]\nhost='${MISSING_DOMAIN}'\nport='web'\n[commands.web]\nserve=['web']\nexec=" + fixtureExec(t, "true") + "\n"
	if e := os.WriteFile(filepath.Join(root, "devtools.toml"), []byte(config), 0600); e != nil {
		t.Fatal(e)
	}
	p, e := project.Resolve(root, "")
	if e != nil {
		t.Fatal(e)
	}
	app := testApp(t)
	q := services.Request{Action: "start", Directory: p.Root, Command: "web", RequestID: tasks.ID()}
	if e := app.prepareManagedNetwork(context.Background(), p, &q, nil, network.PrepareOff, IO{}); e != nil {
		t.Fatal(e)
	}
	if e := app.prepareManagedNetwork(context.Background(), p, &q, nil, network.PrepareAuto, IO{}); e == nil || e.Code != "request_conflict" {
		t.Fatal(e)
	}
	legacy := services.Request{Action: "start", Directory: p.Root, Command: "web", RequestID: tasks.ID()}
	b, _ := json.Marshal(legacy)
	digest := sha256.Sum256(b)
	s, e := app.serviceStore()
	if e != nil {
		t.Fatal(e)
	}
	path := filepath.Join(s.Data, "processes", "receipts", legacy.RequestID+".json")
	if e := tasks.PrivateDir(filepath.Dir(path)); e != nil {
		t.Fatal(e)
	}
	if e := tasks.WritePrivate(path, map[string]any{"fingerprint": hex.EncodeToString(digest[:]), "done": true, "result": services.Result{Item: services.Record{ID: legacy.RequestID}}}); e != nil {
		t.Fatal(e)
	}
	prepareProcessFixture(t, s.Data)
	if e := app.prepareManagedNetwork(context.Background(), p, &legacy, nil, network.PrepareAuto, IO{}); e != nil || legacy.ProxyMode {
		t.Fatal(e)
	}
	result, e := s.Apply(context.Background(), legacy)
	if e != nil || !result.Replayed {
		t.Fatal(result, e)
	}
}
