package expose

import (
	"context"
	"fmt"
	"github.com/jinyongp/devtools/internal/tasks"
	"golang.org/x/sys/windows"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestMain(m *testing.M) {
	if (len(os.Args) == 4 || len(os.Args) == 5) && os.Args[1] == "__exposure-provider" {
		os.Exit(RunCloudflaredSupervisor(context.Background(), os.Args[2], os.Args[3], os.Stdin, os.Stdout, os.Stderr, os.Args[4:]...))
	}
	if len(os.Args) > 1 && os.Args[1] == "__provider-descendant" {
		ignoreProviderBreak()
		for {
			time.Sleep(time.Hour)
		}
	}
	if len(os.Args) > 1 && os.Args[1] == "tunnel" {
		os.Exit(runFakeWindowsProvider())
	}
	if len(os.Args) > 1 && os.Args[1] == "status" {
		if strings.Contains(filepath.Base(os.Args[0]), "oversized") {
			io.WriteString(os.Stdout, strings.Repeat("x", 1<<20))
			os.Exit(0)
		}
		if strings.Contains(filepath.Base(os.Args[0]), "failed") {
			os.Exit(1)
		}
		io.WriteString(os.Stdout, `{"Version":"1.104.1","BackendState":"Running","Self":{"DNSName":"fixture.ts.net"},"CertDomains":["fixture.ts.net"]}`)
		os.Exit(0)
	}
	os.Exit(m.Run())
}

var providerBreakHandler = windows.NewCallback(func(uint32) uintptr { return 1 })

func ignoreProviderBreak() {
	windows.NewLazySystemDLL("kernel32.dll").NewProc("SetConsoleCtrlHandler").Call(providerBreakHandler, 1)
}
func runFakeWindowsProvider() int {
	if len(os.Args) != 5 || os.Args[2] != "--no-autoupdate" || os.Args[3] != "--url" || !ValidOrigin(os.Args[4]) {
		return 2
	}
	mode := filepath.Base(os.Args[0])
	if strings.Contains(mode, "exit") {
		fmt.Fprintln(os.Stderr, "no URL")
		return 1
	}
	if strings.Contains(mode, "early") {
		fmt.Fprintln(os.Stderr, "https://early.trycloudflare.com")
		return 1
	}
	if strings.Contains(mode, "malformed") {
		fmt.Fprintln(os.Stderr, "https://evil.trycloudflare.com.evil.invalid")
	} else {
		if strings.Contains(mode, "tree") {
			ignoreProviderBreak()
			child := exec.Command(os.Args[0], "__provider-descendant")
			if child.Start() != nil {
				return 3
			}
			if os.WriteFile(os.Getenv("DEVTOOLS_TEST_PROVIDER_PID"), []byte(strconv.Itoa(child.Process.Pid)), 0600) != nil {
				return 3
			}
		}
		fmt.Fprintln(os.Stderr, "https://owned-test.trycloudflare.com")
	}
	for {
		time.Sleep(time.Hour)
	}
}
func windowsProvider(t *testing.T, mode string) string {
	t.Helper()
	source, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	return copyWindowsProvider(t, source, mode)
}
func copyWindowsProvider(t *testing.T, source, mode string) string {
	t.Helper()
	body, err := os.ReadFile(source)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), mode+".exe")
	if err = os.WriteFile(path, body, 0600); err != nil {
		t.Fatal(err)
	}
	return path
}
func startWindowsCloudflared(t *testing.T, executable string) (Cloudflared, Record, ProviderSession) {
	t.Helper()
	p := Cloudflared{Data: t.TempDir(), Executable: executable, Timeout: 3 * time.Second}
	r := tailscaleRecord()
	r.Provider = "cloudflared"
	s, failure := p.Start(context.Background(), r, "http://127.0.0.1:31000")
	if failure != nil {
		t.Fatal(failure)
	}
	t.Cleanup(func() { s.Stop(context.Background()) })
	return p, r, s
}
func TestWindowsCloudflaredOwnedLifetimeAndRecovery(t *testing.T) {
	p, r, s := startWindowsCloudflared(t, windowsProvider(t, "working"))
	if s.URL() != "https://owned-test.trycloudflare.com" || s.Owner() != r.Generation || !s.Alive(context.Background()) {
		t.Fatal("invalid provider session")
	}
	stored, failure := (Store{Data: p.Data}).Read(r.ID)
	if failure != nil || stored.ProviderOwner != r.Generation {
		t.Fatal("missing ownership before launch", failure)
	}
	if failure = p.RecoverStop(context.Background(), stored); failure == nil || failure.Code != "provider_recovery_required" {
		t.Fatal("live process accepted as stopped", failure)
	}
	if failure = s.Stop(context.Background()); failure != nil {
		t.Fatal(failure)
	}
	if s.Alive(context.Background()) {
		t.Fatal("provider survived EOF")
	}
	if failure = s.Stop(context.Background()); failure != nil {
		t.Fatal("non-idempotent stop", failure)
	}
	if failure = p.RecoverStop(context.Background(), stored); failure != nil {
		t.Fatal("stopped receipt rejected", failure)
	}
	stored.ProviderOwner = tasks.ID()
	if p.RecoverStop(context.Background(), stored) == nil {
		t.Fatal("foreign owner accepted")
	}
}
func TestWindowsCloudflaredRejectsMissingSpawnMalformedAndEarly(t *testing.T) {
	for _, tc := range []struct{ mode, code string }{
		{"exit", "cloudflared_exited_without_url"}, {"malformed", "cloudflared_url_timeout"}, {"early", "cloudflared_exited"},
	} {
		t.Run(tc.mode, func(t *testing.T) {
			p := Cloudflared{Executable: windowsProvider(t, tc.mode), Timeout: 400 * time.Millisecond}
			s, failure := p.Start(context.Background(), Record{}, "http://127.0.0.1:31000")
			if s != nil {
				s.Stop(context.Background())
			}
			if failure == nil || failure.Code != tc.code {
				t.Fatal(failure)
			}
		})
	}
	p := Cloudflared{Executable: filepath.Join(t.TempDir(), "missing.exe")}
	if _, failure := p.Start(context.Background(), Record{}, "http://127.0.0.1:31000"); failure == nil || failure.Code != "cloudflared_not_installed" {
		t.Fatal(failure)
	}
	p = Cloudflared{Executable: windowsProvider(t, "working"), Supervisor: filepath.Join(t.TempDir(), "missing.exe")}
	if _, failure := p.Start(context.Background(), Record{}, "http://127.0.0.1:31000"); failure == nil || failure.Code != "cloudflared_start_failed" {
		t.Fatal(failure)
	}
	bad := filepath.Join(t.TempDir(), "invalid.exe")
	if err := os.WriteFile(bad, []byte("not PE"), 0600); err != nil {
		t.Fatal(err)
	}
	p = Cloudflared{Executable: bad, Timeout: 2 * time.Second}
	if _, failure := p.Start(context.Background(), Record{}, "http://127.0.0.1:31000"); failure == nil || failure.Code != "cloudflared_exited_without_url" {
		t.Fatal(failure)
	}
}
func providerTreeHandle(t *testing.T) (ProviderSession, windows.Handle) {
	t.Helper()
	pidfile := filepath.Join(t.TempDir(), "child.pid")
	t.Setenv("DEVTOOLS_TEST_PROVIDER_PID", pidfile)
	_, _, s := startWindowsCloudflared(t, windowsProvider(t, "tree"))
	body, err := os.ReadFile(pidfile)
	if err != nil {
		t.Fatal(err)
	}
	pid, err := strconv.Atoi(string(body))
	if err != nil {
		t.Fatal(err)
	}
	handle, err := windows.OpenProcess(windows.SYNCHRONIZE, false, uint32(pid))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { windows.CloseHandle(handle) })
	return s, handle
}
func requireProviderTreeExited(t *testing.T, handle windows.Handle) {
	t.Helper()
	status, err := windows.WaitForSingleObject(handle, 7000)
	if err != nil || status != windows.WAIT_OBJECT_0 {
		t.Fatal("owned descendant survived", status, err)
	}
}
func TestWindowsCloudflaredEOFStopsIgnoringDescendants(t *testing.T) {
	s, handle := providerTreeHandle(t)
	if failure := s.Stop(context.Background()); failure != nil {
		t.Fatal(failure)
	}
	requireProviderTreeExited(t, handle)
}
func TestWindowsCloudflaredSupervisorCrashKillsTree(t *testing.T) {
	s, handle := providerTreeHandle(t)
	if err := s.(*cloudflaredSession).command.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	requireProviderTreeExited(t, handle)
	select {
	case <-s.(*cloudflaredSession).done:
	case <-time.After(3 * time.Second):
		t.Fatal("supervisor output retained after crash")
	}
}
func TestWindowsCloudflaredCallerCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	p := Cloudflared{Executable: windowsProvider(t, "working"), Timeout: 3 * time.Second}
	s, failure := p.Start(ctx, Record{}, "http://127.0.0.1:31000")
	if failure != nil {
		t.Fatal(failure)
	}
	cancel()
	select {
	case <-s.(*cloudflaredSession).done:
	case <-time.After(5 * time.Second):
		t.Fatal("provider survived caller cancellation")
	}
}
func TestWindowsCloudflaredAMD64ProviderOnARM64(t *testing.T) {
	if runtime.GOARCH != "arm64" {
		t.Log("amd64 host uses native provider in other required cases")
		return
	}
	source := os.Getenv("DEVTOOLS_TEST_X64_PROVIDER")
	if source == "" {
		t.Fatal("required amd64 emulation fixture missing")
	}
	_, _, s := startWindowsCloudflared(t, copyWindowsProvider(t, source, "emulated-working"))
	t.Log("devtools native architecture=arm64; provider executable architecture=amd64; Windows emulation")
	if failure := s.Stop(context.Background()); failure != nil {
		t.Fatal(failure)
	}
}
func TestWindowsTailscaleNativeCLIOutputAndErrors(t *testing.T) {
	for _, tc := range []struct{ mode, code string }{{"working", ""}, {"failed", "tailscale_command_failed"}, {"oversized", "tailscale_status_invalid"}} {
		t.Run(tc.mode, func(t *testing.T) {
			p := Tailscale{Executable: windowsProvider(t, tc.mode)}
			_, failure := p.node(context.Background())
			if tc.code == "" {
				if failure != nil {
					t.Fatal(failure)
				}
			} else if failure == nil || failure.Code != tc.code {
				t.Fatal(failure)
			}
		})
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, failure := (Tailscale{Executable: windowsProvider(t, "working")}).run(ctx, "status", "--json"); failure == nil || failure.Code != "tailscale_timeout" {
		t.Fatal(failure)
	}
}
