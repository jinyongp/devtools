package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
)

func TestCommandContract(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
		exit int
		code string
	}{
		{"default", nil, 0, ""},
		{"schema", []string{"schema"}, 0, ""},
		{"init missing profile", []string{"init"}, 2, "invalid_argument"},
		{"init help", []string{"init", "--help"}, 0, ""},
		{"version", []string{"--version"}, 0, ""},
		{"help", []string{"project", "inspect", "--help"}, 0, ""},
		{"explicit", []string{"project", "inspect", "--profile", "myapp"}, 0, ""},
		{"unknown", []string{"secret"}, 2, "invalid_argument"},
		{"unknown flag", []string{"version", "--token=DO_NOT_ECHO"}, 2, "invalid_argument"},
		{"positional", []string{"version", "extra"}, 2, "invalid_argument"},
		{"invalid profile", []string{"project", "inspect", "--profile=../bad"}, 2, "invalid_argument"},
		{"empty profile", []string{"project", "inspect", "--profile="}, 2, "invalid_argument"},
		{"empty directory", []string{"project", "inspect", "--dir="}, 2, "invalid_argument"},
		{"missing flag value", []string{"project", "inspect", "--dir"}, 2, "invalid_argument"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var out, diagnostic bytes.Buffer
			exit := New("test", "abc").Run(context.Background(), tc.args, IO{In: bytes.NewReader(nil), Out: &out, Err: &diagnostic})
			if exit != tc.exit {
				t.Fatalf("exit=%d stdout=%s stderr=%s", exit, &out, &diagnostic)
			}
			payload := &out
			if exit != 0 {
				payload = &diagnostic
				if out.Len() != 0 {
					t.Fatal("failure wrote stdout")
				}
			} else if diagnostic.Len() != 0 {
				t.Fatal("success wrote stderr")
			}
			if bytes.Contains(payload.Bytes(), []byte("DO_NOT_ECHO")) {
				t.Fatal("echoed unrecognized argument")
			}
			var response struct {
				SchemaVersion int             `json:"schema_version"`
				OK            bool            `json:"ok"`
				Data          json.RawMessage `json:"data"`
				Error         struct {
					Code string `json:"code"`
				} `json:"error"`
			}
			decoder := json.NewDecoder(payload)
			if err := decoder.Decode(&response); err != nil {
				t.Fatal(err)
			}
			if response.SchemaVersion != 1 || response.OK != (exit == 0) || response.Error.Code != tc.code {
				t.Fatalf("response: %+v", response)
			}
			if err := decoder.Decode(new(any)); err != io.EOF {
				t.Fatalf("extra output: %v", err)
			}
		})
	}
}

func TestInspectAndCancellation(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "devtools.toml"), []byte("profile='test-project'\n"), 0600); err != nil {
		t.Fatal(err)
	}
	var out, diagnostic bytes.Buffer
	app := New("test", "abc")
	if exit := app.Run(context.Background(), []string{"project", "inspect", "--dir", root}, IO{Out: &out, Err: &diagnostic}); exit != 0 {
		t.Fatal(diagnostic.String())
	}
	if !bytes.Contains(out.Bytes(), []byte(`"profile":"test-project"`)) {
		t.Fatal(out.String())
	}
	out.Reset()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if exit := app.Run(ctx, []string{"version"}, IO{Out: &out, Err: &diagnostic}); exit != 130 || out.Len() != 0 {
		t.Fatalf("cancel: %d %s", exit, &out)
	}
}

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, errors.New("write failed") }

func TestOutputFailure(t *testing.T) {
	var diagnostic bytes.Buffer
	if exit := New("test", "abc").Run(context.Background(), []string{"version"}, IO{Out: failingWriter{}, Err: &diagnostic}); exit != 1 {
		t.Fatalf("exit=%d", exit)
	}
	if !bytes.Contains(diagnostic.Bytes(), []byte(`"code":"io_error"`)) {
		t.Fatal(diagnostic.String())
	}
}
