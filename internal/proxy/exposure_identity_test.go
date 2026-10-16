package proxy

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/jinyongp/devtools/internal/network"
	"github.com/jinyongp/devtools/internal/project"
)

func TestExposureIdentityTracksLiteralRouteSourcesAndConflicts(t *testing.T) {
	ctx := context.Background()
	m := Manager{Data: t.TempDir()}
	root := t.TempDir()
	config := "profile='app'\nenv_files=['.env']\n[ports.web]\nport=31000\n[proxies.app]\nhost='app.localhost'\nport='web'\n"
	if e := os.WriteFile(filepath.Join(root, "devtools.toml"), []byte(config), 0600); e != nil {
		t.Fatal(e)
	}
	path := filepath.Join(root, ".env")
	original := []byte("IGNORED=value\n")
	if e := os.WriteFile(path, original, 0600); e != nil {
		t.Fatal(e)
	}
	instance := register(t, m.portStore(), "app", root, nil, 31000)
	ref := network.RouteRef{Scope: "project", Profile: "app", InstanceID: instance.ID, Proxy: "app"}
	if _, e := m.portStore().ConfigureListener(ctx, "default", fmt.Sprintf("127.0.0.1:%d", freeNetworkPort(t)), "", false); e != nil {
		t.Fatal(e)
	}
	item, listener, before, e := m.exposureIdentity(ctx, ref)
	if e != nil {
		t.Fatal(e)
	}
	p, e := project.Resolve(root, "")
	if e != nil {
		t.Fatal(e)
	}
	source, e := p.NetworkSources()
	if e != nil {
		t.Fatal(e)
	}
	st, e := m.portStore().Read()
	if e != nil {
		t.Fatal(e)
	}
	httpPort, httpsPort, e := st.ListenerPorts("default")
	if e != nil {
		t.Fatal(e)
	}
	// Persisted exposure identities must keep the pre-snapshot digest projection.
	expected := project.Digest(struct {
		Item        Item
		Listener    network.Listener
		HTTP, HTTPS int
		Source      string
	}{item, listener, httpPort, httpsPort, source})
	if before != expected {
		t.Fatal("persisted identity projection changed")
	}
	if e := os.WriteFile(path, []byte("IGNORED=changed\n"), 0600); e != nil {
		t.Fatal(e)
	}
	_, _, after, e := m.exposureIdentity(ctx, ref)
	if e != nil || after == before {
		t.Fatal("literal route source edit was missed", e)
	}
	if e := os.Remove(path); e != nil {
		t.Fatal(e)
	}
	if e := os.Mkdir(path, 0700); e != nil {
		t.Fatal(e)
	}
	if _, _, _, e := m.exposureIdentity(ctx, ref); e == nil || e.Code != "invalid_config" {
		t.Fatal("source read failure was hidden", e)
	}
	if e := os.Remove(path); e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(path, original, 0600); e != nil {
		t.Fatal(e)
	}
	other := t.TempDir()
	writeConfig(t, other, "app", "app.localhost")
	register(t, m.portStore(), "app", other, nil, 31001)
	if _, _, _, e := m.exposureIdentity(ctx, ref); e == nil || e.Code != "route_unavailable" {
		t.Fatal("conflicting project host was ignored", e)
	}
}
