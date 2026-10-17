//go:build !windows

package expose

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestMain(m *testing.M) {
	if (len(os.Args) == 4 || len(os.Args) == 5) && os.Args[1] == "__exposure-provider" {
		os.Exit(RunCloudflaredSupervisor(context.Background(), os.Args[2], os.Args[3], os.Stdin, os.Stdout, os.Stderr, os.Args[4:]...))
	}
	os.Exit(m.Run())
}
func fakeCloudflared(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "cloudflared")
	if e := os.WriteFile(path, []byte("#!/bin/sh\n"+body), 0700); e != nil {
		t.Fatal(e)
	}
	return path
}
func TestCloudflaredOwnsLifetimeURLAndStop(t *testing.T) {
	executable := fakeCloudflared(t, `test "$1" = tunnel || exit 2
test "$2" = --no-autoupdate || exit 2
test "$3" = --url || exit 2
echo 'https://owned-test.trycloudflare.com' >&2
trap 'exit 0' TERM INT HUP
while :; do sleep 1; done
`)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s, e := (Cloudflared{Executable: executable, Timeout: time.Second}).Start(ctx, Record{Public: Public{Generation: "owned-generation"}}, "http://127.0.0.1:31000")
	if e != nil {
		t.Fatal(e)
	}
	if s.URL() != "https://owned-test.trycloudflare.com" || s.Owner() != "owned-generation" || !s.Alive(ctx) {
		t.Fatal("invalid owned session")
	}
	if e := s.Stop(ctx); e != nil {
		t.Fatal(e)
	}
	if s.Alive(ctx) {
		t.Fatal("provider survived stop")
	}
	if e := s.Stop(ctx); e != nil {
		t.Fatal("stop is not idempotent", e)
	}
}
func TestCloudflaredRejectsMissingMalformedAndEarlyExit(t *testing.T) {
	for _, tc := range []struct{ name, body, code string }{
		{"exit", "echo no-url; exit 1\n", "cloudflared_exited_without_url"},
		{"malformed", "echo https://evil.trycloudflare.com.evil.example; sleep 5\n", "cloudflared_url_timeout"},
		{"early", "echo https://early.trycloudflare.com; exit 1\n", "cloudflared_exited"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, e := (Cloudflared{Executable: fakeCloudflared(t, tc.body), Timeout: 250 * time.Millisecond}).Start(context.Background(), Record{}, "http://127.0.0.1:31000")
			if s != nil {
				_ = s.Stop(context.Background())
			}
			if e == nil || e.Code != tc.code {
				t.Fatal(e)
			}
		})
	}
}
