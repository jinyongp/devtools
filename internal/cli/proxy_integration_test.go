package cli

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type cliEnvelope struct {
	OK    bool            `json:"ok"`
	Data  json.RawMessage `json:"data"`
	Error struct {
		Code string `json:"code"`
	} `json:"error"`
}

func isolatedEnv(home string) []string {
	environment := []string{}
	for _, entry := range os.Environ() {
		if !strings.HasPrefix(entry, "HOME=") {
			environment = append(environment, entry)
		}
	}
	return append(environment, "HOME="+home)
}

func externalCLI(t *testing.T, binary, home, directory string, args ...string) cliEnvelope {
	t.Helper()
	command := exec.Command(binary, args...)
	command.Dir = directory
	command.Env = isolatedEnv(home)
	output, err := command.Output()
	if err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			t.Fatalf("devtools %v: %s", args, exit.Stderr)
		}
		t.Fatal(err)
	}
	var envelope cliEnvelope
	if err := json.Unmarshal(output, &envelope); err != nil || !envelope.OK {
		t.Fatalf("devtools %v: %s %v", args, output, err)
	}
	return envelope
}

func externalCLIFailure(t *testing.T, binary, home, directory string, args ...string) string {
	t.Helper()
	command := exec.Command(binary, args...)
	command.Dir = directory
	command.Env = isolatedEnv(home)
	var output, diagnostic bytes.Buffer
	command.Stdout, command.Stderr = &output, &diagnostic
	if err := command.Run(); err == nil || output.Len() != 0 {
		t.Fatalf("devtools %v unexpectedly succeeded: %s", args, &output)
	}
	var envelope cliEnvelope
	if err := json.Unmarshal(diagnostic.Bytes(), &envelope); err != nil {
		t.Fatalf("devtools %v diagnostic: %s", args, &diagnostic)
	}
	return envelope.Error.Code
}

func integrationBackend(t *testing.T, port int, label string) *httptest.Server {
	t.Helper()
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/socket" {
			connection, buffer, err := response.(http.Hijacker).Hijack()
			if err != nil {
				t.Error(err)
				return
			}
			defer connection.Close()
			_, _ = buffer.WriteString("HTTP/1.1 101 Switching Protocols\r\nConnection: Upgrade\r\nUpgrade: websocket\r\n\r\n" + label)
			_ = buffer.Flush()
			return
		}
		_, _ = io.WriteString(response, label)
	}))
	listener, err := net.Listen("tcp4", net.JoinHostPort("127.0.0.1", fmt.Sprint(port)))
	if err != nil {
		t.Fatal(err)
	}
	server.Listener = listener
	server.Start()
	return server
}

func integrationPort(t *testing.T, server *httptest.Server) int {
	t.Helper()
	return server.Listener.Addr().(*net.TCPAddr).Port
}

func cliFreePortExcept(t *testing.T, excluded ...int) int {
	t.Helper()
	for {
		port := cliFreePort(t)
		available := true
		for _, blocked := range excluded {
			if port == blocked {
				available = false
				break
			}
		}
		if available {
			return port
		}
	}
}

func writeProxyFixture(t *testing.T, directory string, port int) {
	t.Helper()
	content := fmt.Sprintf("profile='shop'\n[ports.web]\nport=%d\nstrict=true\n[proxies.app]\nhost='${instance.alias}.${profile}.localhost'\nport='web'\n", port)
	if err := os.WriteFile(filepath.Join(directory, "devtools.toml"), []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
}

func proxyRequest(t *testing.T, port int, host, path string) string {
	t.Helper()
	request, _ := http.NewRequest(http.MethodGet, "http://127.0.0.1:"+fmt.Sprint(port)+path, nil)
	request.Host = host
	client := http.Client{Timeout: 2 * time.Second, Transport: &http.Transport{Proxy: nil}}
	defer client.CloseIdleConnections()
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, _ := io.ReadAll(response.Body)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("%s: status=%d body=%s", host, response.StatusCode, body)
	}
	return string(body)
}

func TestProxyBinaryEndToEnd(t *testing.T) {
	if testing.Short() {
		t.Skip("builds and runs a detached binary")
	}
	repository, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	binary := filepath.Join(t.TempDir(), "devtools")
	build := exec.Command("go", "build", "-o", binary, "./cmd/devtools")
	build.Dir = repository
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %s %v", output, err)
	}
	home := t.TempDir()
	mainDirectory, featureDirectory := t.TempDir(), t.TempDir()
	mainPort := cliFreePort(t)
	writeProxyFixture(t, mainDirectory, mainPort)
	externalCLI(t, binary, home, mainDirectory, "port", "allocate", "web")
	externalCLI(t, binary, home, mainDirectory, "instance", "name", "main")
	featurePort := cliFreePortExcept(t, mainPort)
	writeProxyFixture(t, featureDirectory, featurePort)
	externalCLI(t, binary, home, featureDirectory, "port", "allocate", "web")
	externalCLI(t, binary, home, featureDirectory, "instance", "name", "feature")
	mainBackend, featureBackend := integrationBackend(t, mainPort, "main"), integrationBackend(t, featurePort, "feature")
	defer featureBackend.Close()
	listenerPort := cliFreePortExcept(t, mainPort, featurePort)
	started := false
	defer func() {
		if started {
			command := exec.Command(binary, "proxy", "stop", "--request-id", "f0f0f0f0-f0f0-40f0-80f0-f0f0f0f0f0f0")
			command.Dir, command.Env = mainDirectory, isolatedEnv(home)
			_ = command.Run()
		}
	}()
	start := externalCLI(t, binary, home, mainDirectory, "proxy", "start", "--port", fmt.Sprint(listenerPort), "--request-id", "a0a0a0a0-a0a0-40a0-80a0-a0a0a0a0a0a0")
	started = true
	var initial struct {
		Item struct {
			Running   bool      `json:"running"`
			Port      int       `json:"port"`
			StartedAt time.Time `json:"started_at"`
		} `json:"item"`
	}
	if json.Unmarshal(start.Data, &initial) != nil || !initial.Item.Running || initial.Item.Port != listenerPort {
		t.Fatalf("start = %s", start.Data)
	}
	if body := proxyRequest(t, listenerPort, "main.shop.localhost", "/"); body != "main" {
		t.Fatalf("main route = %q", body)
	}
	if body := proxyRequest(t, listenerPort, "feature.shop.localhost", "/"); body != "feature" {
		t.Fatalf("feature route = %q", body)
	}

	connection, err := net.DialTimeout("tcp4", net.JoinHostPort("127.0.0.1", fmt.Sprint(listenerPort)), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = fmt.Fprint(connection, "GET /socket HTTP/1.1\r\nHost: main.shop.localhost\r\nConnection: Upgrade\r\nUpgrade: websocket\r\n\r\n")
	websocket, err := http.ReadResponse(bufio.NewReader(connection), nil)
	_ = connection.Close()
	if err != nil || websocket.StatusCode != http.StatusSwitchingProtocols {
		t.Fatalf("websocket = %+v %v", websocket, err)
	}

	mainBackend.Close()
	externalCLI(t, binary, home, mainDirectory, "port", "release", "web")
	replacementPort := cliFreePortExcept(t, mainPort, featurePort, listenerPort)
	writeProxyFixture(t, mainDirectory, replacementPort)
	externalCLI(t, binary, home, mainDirectory, "port", "allocate", "web")
	replacement := integrationBackend(t, replacementPort, "replacement")
	defer replacement.Close()
	if body := proxyRequest(t, listenerPort, "main.shop.localhost", "/"); body != "replacement" {
		t.Fatalf("dynamic route = %q", body)
	}
	status := externalCLI(t, binary, home, mainDirectory, "proxy", "status")
	var current struct {
		Item struct {
			StartedAt time.Time `json:"started_at"`
		} `json:"item"`
	}
	if err := json.Unmarshal(status.Data, &current); err != nil {
		t.Fatal(err)
	}
	if !current.Item.StartedAt.Equal(initial.Item.StartedAt) {
		t.Fatalf("assignment change restarted daemon: %v != %v", current.Item.StartedAt, initial.Item.StartedAt)
	}

	externalCLI(t, binary, home, mainDirectory, "proxy", "stop", "--request-id", "b0b0b0b0-b0b0-40b0-80b0-b0b0b0b0b0b0")
	started = false
	other := t.TempDir()
	content := fmt.Sprintf("profile='other'\n[ports.listener]\nport=%d\nstrict=true\n", listenerPort)
	if err := os.WriteFile(filepath.Join(other, "devtools.toml"), []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	if code := externalCLIFailure(t, binary, home, other, "port", "allocate", "listener"); code != "port_in_use" {
		t.Fatalf("stopped reservation allocation = %s", code)
	}
	restart := externalCLI(t, binary, home, mainDirectory, "proxy", "start", "--request-id", "c0c0c0c0-c0c0-40c0-80c0-c0c0c0c0c0c0")
	started = true
	var restarted struct {
		Item struct {
			Port int `json:"port"`
		} `json:"item"`
	}
	if err := json.Unmarshal(restart.Data, &restarted); err != nil {
		t.Fatal(err)
	}
	if restarted.Item.Port != listenerPort || proxyRequest(t, listenerPort, "main.shop.localhost", "/") != "replacement" {
		t.Fatalf("restart = %s", restart.Data)
	}

	if probe, err := net.Listen("tcp6", "[::1]:0"); err == nil {
		_ = probe.Close()
		ipv6, err := net.DialTimeout("tcp6", net.JoinHostPort("::1", fmt.Sprint(listenerPort)), time.Second)
		if err != nil {
			t.Fatalf("IPv6 listener: %v", err)
		}
		_, _ = fmt.Fprint(ipv6, "GET / HTTP/1.1\r\nHost: feature.shop.localhost\r\nConnection: close\r\n\r\n")
		response, err := http.ReadResponse(bufio.NewReader(ipv6), nil)
		_ = ipv6.Close()
		if err != nil || response.StatusCode != http.StatusOK {
			t.Fatalf("IPv6 response = %+v %v", response, err)
		}
	}
}
