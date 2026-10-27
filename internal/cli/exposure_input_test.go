package cli

import (
	"bytes"
	"context"
	"io"
	"strings"
	"testing"
	"time"
)

func TestExposureStaticInputsPrecedeRouteAndAuth(t *testing.T) {
	app := testApp(t)
	for _, flags := range [][]string{
		{"--auth-stdin"},
		{"--provider", "lan", "--auth-stdin", "--auth-file", "absent"},
		{"--provider", "lan", "--auth-stdin", "--no-auth"},
		{"--provider", "lan", "--auth-stdin", "--serve-port", "443"},
		{"--provider", "tailscale", "--auth-stdin", "--alias", "wrong.local"},
		{"--provider", "cloudflared", "--auth-stdin", "--serve-port", "443"},
		{"--provider", "lan", "--auth-stdin", "--serve-port", "65536"},
	} {
		reader := &observedSecretReader{}
		var out, diagnostic bytes.Buffer
		args := append([]string{"expose", "start", "missing", "--global", "--request-id", "00000000-0000-4000-8000-000000000001"}, flags...)
		code := app.Run(context.Background(), args, IO{In: reader, Out: &out, Err: &diagnostic})
		if code != 2 || reader.read || !strings.Contains(diagnostic.String(), "invalid_argument") {
			t.Fatal(args, code, reader.read, &diagnostic)
		}
	}
}

func TestExposureAuthCancellationAndLimit(t *testing.T) {
	r, w := io.Pipe()
	defer w.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if auth, e := readExposureAuth(ctx, IO{In: r}, Request{Options: map[string]string{"auth-stdin": "true"}}); auth != "" || e == nil || e.ExitCode != 130 {
		t.Fatal(auth, e)
	}
	if _, err := w.Write([]byte("must-not-read")); err != io.ErrClosedPipe {
		t.Fatal("input worker retained pipe", err)
	}
	for _, size := range []int{8192, 8193, 8194} {
		auth, e := readExposureAuth(context.Background(), IO{In: strings.NewReader(strings.Repeat("x", size))}, Request{Options: map[string]string{"auth-stdin": "true"}})
		if size <= 8193 {
			if e != nil || len(auth) != size {
				t.Fatal(size, e)
			}
		} else if e == nil || e.Code != "invalid_authentication" {
			t.Fatal(size, e)
		}
	}
	for _, suffix := range []string{"\n", "\r\n", ""} {
		auth, e := readExposureAuth(context.Background(), IO{In: strings.NewReader("user:private" + suffix)}, Request{Options: map[string]string{"auth-stdin": "true"}})
		if e != nil || auth != "user:private" {
			t.Fatal(e)
		}
	}
}
