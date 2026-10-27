package cli

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jinyongp/devtools/internal/tasks"
	"github.com/jinyongp/devtools/internal/vault"
)

type countedInput struct{ remaining, read int64 }

func (r *countedInput) Read(p []byte) (int, error) {
	if r.remaining == 0 {
		return 0, io.EOF
	}
	n := min(int64(len(p)), r.remaining)
	for i := range p[:n] {
		p[i] = 'x'
	}
	r.remaining -= n
	r.read += n
	return int(n), nil
}

func TestBoundedValueInput(t *testing.T) {
	for _, size := range []int64{63, 64, 65, 1000000} {
		r := &countedInput{remaining: size}
		value, e := readValueInputLimit(context.Background(), IO{In: r}, map[string]string{"stdin": "true"}, "variable", 64, false)
		if size <= 64 {
			if e != nil || int64(len(value)) != size {
				t.Fatal(size, e)
			}
		} else {
			if e == nil || e.Code != "vault_input_too_large" || e.ExitCode != 3 || value != "" {
				t.Fatal(size, e)
			}
		}
		if r.read != min(size, 65) {
			t.Fatal("read beyond limit+1", r.read)
		}
	}
	file := filepath.Join(privateTempDir(t), "large")
	f, err := os.Create(file)
	if err != nil {
		t.Fatal(err)
	}
	if err = f.Truncate(vault.MaxDocumentBytes + 1); err != nil {
		t.Fatal(err)
	}
	f.Close()
	if _, e := readValueInput(context.Background(), IO{}, map[string]string{"file": file}, "dotenv"); e == nil || e.Code != "vault_input_too_large" || e.Details["facts"].(map[string]any)["observed_exact"] != true {
		t.Fatal(e)
	}
}

func TestRawInputRealCeiling(t *testing.T) {
	if testing.Short() {
		t.Skip("128 MiB input boundary")
	}
	r := &countedInput{remaining: vault.MaxDocumentBytes + 1000}
	value, e := readValueInput(context.Background(), IO{In: r}, map[string]string{"stdin": "true"}, "variable")
	if value != "" || e == nil || e.Code != "vault_input_too_large" || r.read != vault.MaxDocumentBytes+1 {
		t.Fatal("raw ceiling", r.read, e)
	}
}

func TestBoundedInputCancellationClosesWorker(t *testing.T) {
	for i := 0; i < 20; i++ {
		r, w := io.Pipe()
		ctx, cancel := context.WithCancel(context.Background())
		ended := make(chan struct{})
		go func() {
			defer close(ended)
			body, e := readBoundedInput(ctx, r, 64, "secret")
			if body != nil || e == nil || e.ExitCode != 130 {
				t.Errorf("cancel returned %v", e)
			}
		}()
		cancel()
		select {
		case <-ended:
		case <-time.After(time.Second):
			t.Fatal("read did not cancel")
		}
		w.Close()
		r.Close()
	}
}

func TestManagedImportReadBudget(t *testing.T) {
	for _, guarded := range []bool{false, true} {
		for _, dry := range []bool{false, true} {
			for _, size := range []int{500000, 500001} {
				app := testApp(t)
				file := filepath.Join(privateTempDir(t), "input.env")
				content := "#" + strings.Repeat("x", size-len("\nURL=ok\n")-1) + "\nURL=ok\n"
				if err := os.WriteFile(file, []byte(content), 0600); err != nil {
					t.Fatal(err)
				}
				args := []string{"import", "--profile", "app", "--file", file, "--var", "URL"}
				if guarded {
					args = append(args, "--revision", editToken(t, app), "--request-id", tasks.ID())
				} else {
					args = append(args, "--bundle", "web")
				}
				if dry {
					args = append(args, "--dry-run")
				}
				code, _, diagnostic := invoke(t, app, "", args...)
				if size == 500001 {
					if code != 2 || !strings.Contains(diagnostic, "invalid_argument") {
						t.Fatal(code, diagnostic)
					}
				} else if code != 0 {
					t.Fatal(code, diagnostic)
				}
			}
		}
	}
}
