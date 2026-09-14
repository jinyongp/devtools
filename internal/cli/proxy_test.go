package cli

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"

	proxyapi "github.com/jinyongp/devtools/internal/proxy"
)

func cliFreePort(t *testing.T) int {
	t.Helper()
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	_ = listener.Close()
	return port
}

func TestProxyCLIStartListStatusAndStop(t *testing.T) {
	app := testApp(t)
	dataDirectory, _ := app.dataDirectory()
	manager := proxyapi.Manager{Data: filepath.Dir(dataDirectory)}
	manager.Spawn = func(_ string, attemptID string) error {
		go func() { _ = manager.Serve(context.Background(), attemptID) }()
		return nil
	}
	app.proxyManager = func(string) proxyapi.Manager { return manager }

	root := t.TempDir()
	t.Chdir(root)
	backendPort := cliFreePort(t)
	config := fmt.Sprintf("profile='app'\n[ports.web]\nport=%d\nstrict=true\n[proxies.app]\nhost='${instance.alias}.${profile}.localhost'\nport='web'\n", backendPort)
	if err := os.WriteFile("devtools.toml", []byte(config), 0600); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"port", "allocate", "web"}, {"instance", "name", "main"}} {
		if code, _, diagnostic := invoke(t, app, "", args...); code != 0 {
			t.Fatalf("%v: %s", args, diagnostic)
		}
	}
	listenerPort := cliFreePort(t)
	startID := "10101010-1010-4010-8010-101010101010"
	code, output, diagnostic := invoke(t, app, "", "proxy", "start", "--port", fmt.Sprint(listenerPort), "--request-id", startID)
	if code != 0 || diagnostic != "" || !strings.Contains(output, `"running":true`) || !strings.Contains(output, fmt.Sprintf(`"port":%d`, listenerPort)) {
		t.Fatalf("start: %d %s %s", code, output, diagnostic)
	}
	code, output, diagnostic = invoke(t, app, "", "proxy", "status")
	if code != 0 || diagnostic != "" || !strings.Contains(output, `"state":"running"`) {
		t.Fatalf("status: %d %s %s", code, output, diagnostic)
	}
	code, output, diagnostic = invoke(t, app, "", "proxy", "list", "--profile", "app")
	if code != 0 || diagnostic != "" || !strings.Contains(output, `"host":"main.app.localhost"`) || !strings.Contains(output, `"status":"ready"`) {
		t.Fatalf("list: %d %s %s", code, output, diagnostic)
	}
	code, output, diagnostic = invoke(t, app, "", "proxy", "stop", "--request-id", "20202020-2020-4020-8020-202020202020")
	if code != 0 || diagnostic != "" || !strings.Contains(output, `"state":"stopped"`) || !strings.Contains(output, fmt.Sprintf(`"port":%d`, listenerPort)) {
		t.Fatalf("stop: %d %s %s", code, output, diagnostic)
	}
}

func TestProxyCLIContractAndFailures(t *testing.T) {
	app := testApp(t)
	for _, test := range []struct {
		args []string
		exit int
		want string
	}{
		{args: []string{"proxy", "start"}, exit: 2, want: "invalid_argument"},
		{args: []string{"proxy", "start", "--port", "70000", "--request-id", "30303030-3030-4030-8030-303030303030"}, exit: 2, want: `"field":"port"`},
		{args: []string{"proxy", "stop"}, exit: 2, want: "invalid_argument"},
		{args: []string{"proxy", "list", "--profile", "../bad"}, exit: 2, want: "invalid_argument"},
	} {
		code, output, diagnostic := invoke(t, app, "", test.args...)
		if code != test.exit || output != "" || !strings.Contains(diagnostic, test.want) {
			t.Errorf("%v: %d %s %s", test.args, code, output, diagnostic)
		}
	}
	for _, args := range [][]string{{"proxy", "start", "--help"}, {"schema", "proxy", "list"}} {
		code, output, diagnostic := invoke(t, app, "", args...)
		if code != 0 || diagnostic != "" || !strings.Contains(output, "proxy") {
			t.Errorf("%v: %d %s %s", args, code, output, diagnostic)
		}
	}
}

func TestProxyCLIReportsStartFailure(t *testing.T) {
	app := testApp(t)
	dataDirectory, _ := app.dataDirectory()
	manager := proxyapi.Manager{Data: filepath.Dir(dataDirectory), Spawn: func(string, string) error { return errors.New("injected") }}
	app.proxyManager = func(string) proxyapi.Manager { return manager }
	port := cliFreePort(t)
	code, output, diagnostic := invoke(t, app, "", "proxy", "start", "--port", fmt.Sprint(port), "--request-id", "40404040-4040-4040-8040-404040404040")
	if code != 3 || output != "" || !strings.Contains(diagnostic, `"code":"proxy_start_failed"`) {
		t.Fatalf("start failure: %d %s %s", code, output, diagnostic)
	}
	code, output, diagnostic = invoke(t, app, "", "proxy", "status")
	if code != 0 || diagnostic != "" || !strings.Contains(output, `"state":"failed"`) || !strings.Contains(output, `"reason":"supervisor_start_failed"`) {
		t.Fatalf("failed status: %d %s %s", code, output, diagnostic)
	}
}

func TestRootHelpDescribesProxyGroup(t *testing.T) {
	code, output, diagnostic := invoke(t, New("test", "test"), "", "--help")
	if code != 0 || diagnostic != "" || !strings.Contains(output, "proxy") || !strings.Contains(output, "reverse proxy") {
		t.Fatalf("root help: %d %s %s", code, output, diagnostic)
	}
}
