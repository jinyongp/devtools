package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/jinyongp/devtools/internal/cli/presentation"
	"github.com/jinyongp/devtools/internal/tasks"
)

func TestInitCommand(t *testing.T) {
	t.Chdir(privateTempDir(t))
	app := New("test", "abc")
	for _, expected := range []bool{true, false} {
		var out, diagnostic bytes.Buffer
		code := app.Run(context.Background(), []string{"init", "--profile", "myapp"}, IO{Out: &out, Err: &diagnostic})
		if code != 0 || diagnostic.Len() != 0 {
			t.Fatalf("init: %d %s", code, &diagnostic)
		}
		var response struct {
			Data struct {
				Changed bool `json:"changed"`
				Item    struct {
					Profile string `json:"profile"`
				} `json:"item"`
			} `json:"data"`
		}
		if err := json.Unmarshal(out.Bytes(), &response); err != nil {
			t.Fatal(err)
		}
		if response.Data.Changed != expected || response.Data.Item.Profile != "myapp" {
			t.Fatalf("response: %+v", response)
		}
	}
}

type failedInitOutput struct{}

func (failedInitOutput) Write([]byte) (int, error) { return 0, io.ErrClosedPipe }

func TestInitLostOutputConvergesOnExistingConfiguration(t *testing.T) {
	t.Chdir(privateTempDir(t))
	app := New("test", "test")
	var diagnostic bytes.Buffer
	code := app.Run(context.Background(), []string{"--format", "json", "init", "--profile", "shared"}, IO{Out: failedInitOutput{}, Err: &diagnostic})
	var failure struct {
		Error struct {
			Details struct{ Effects, Retry string }
		}
	}
	if code != 1 || json.Unmarshal(diagnostic.Bytes(), &failure) != nil || failure.Error.Details.Effects != "unknown" || failure.Error.Details.Retry != "inspect_first" {
		t.Fatalf("%d %s", code, &diagnostic)
	}
	before, err := os.ReadFile("devtools.toml")
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	diagnostic.Reset()
	code = app.Run(context.Background(), []string{"--format", "json", "init"}, IO{In: forbiddenInitInput{}, Out: &out, Err: &diagnostic})
	var response struct {
		Data struct {
			Changed bool
			Item    struct {
				Profile string
				Source  string `json:"profile_source"`
			}
		}
	}
	after, err := os.ReadFile("devtools.toml")
	if code != 0 || err != nil || json.Unmarshal(out.Bytes(), &response) != nil || response.Data.Changed || response.Data.Item.Profile != "shared" || response.Data.Item.Source != "config" || !bytes.Equal(before, after) {
		t.Fatalf("retry: %d %s %s", code, &out, &diagnostic)
	}
}

type forbiddenInitInput struct{}

func (forbiddenInitInput) Read([]byte) (int, error) { panic("noninteractive init read stdin") }

func TestInitDirectorySelectionDoesNotReadInput(t *testing.T) {
	for _, tc := range []struct {
		name, profile, config, storage, code, source, presence string
		changed                                                bool
	}{
		{name: "demo-web", source: "directory", presence: "absent", changed: true},
		{name: "invalid name", code: "invalid_argument"},
		{name: "existing", config: "profile='shared'\n", storage: "unprepared", source: "config", presence: "unchecked"},
		{name: "explicit", profile: "shared", storage: "unprepared", source: "flag", presence: "unchecked", changed: true},
		{name: "broken-config", config: "broken=[", code: "invalid_config"},
		{name: "conflict", profile: "other", config: "profile='shared'", code: "profile_conflict"},
		{name: "same", profile: "shared", config: "# preserve\nprofile='shared'", source: "flag", presence: "unchecked"},
		{name: "unprepared", storage: "unprepared", code: "storage_error"},
		{name: "collision", storage: "profile", code: "profile_name_in_use"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			base := privateTempDir(t)
			directory := filepath.Join(base, tc.name)
			if err := os.Mkdir(directory, 0700); err != nil {
				t.Fatal(err)
			}
			config := filepath.Join(directory, "devtools.toml")
			if tc.config != "" {
				if err := os.WriteFile(config, []byte(tc.config), 0600); err != nil {
					t.Fatal(err)
				}
			}
			app := New("test", "test")
			app.isolatedRoot = filepath.Join(base, "managed")
			var output, diagnostic bytes.Buffer
			streams := IO{In: forbiddenInitInput{}, Out: &output, Err: &diagnostic}
			if tc.storage == "unprepared" {
				if err := os.MkdirAll(filepath.Join(app.isolatedRoot, "data", "profiles"), 0700); err != nil {
					t.Fatal(err)
				}
			} else if tc.storage == "profile" {
				if code := app.Run(context.Background(), []string{"--isolated-root", app.isolatedRoot, "task", "add", "--title", "Synthetic profile", "--profile", tc.name, "--request-id", tasks.ID()}, streams); code != 0 {
					t.Fatal(diagnostic.String())
				}
				output.Reset()
				diagnostic.Reset()
			}
			options := map[string]string{}
			if tc.profile != "" {
				options["profile"] = tc.profile
			}
			result, failure := app.initDirectory(context.Background(), streams, Request{Options: options, OutputFormat: presentation.FormatJSON}, directory)
			if tc.code != "" {
				if failure == nil || failure.Code != tc.code {
					t.Fatalf("result=%v failure=%#v", result, failure)
				}
			} else {
				if failure != nil {
					t.Fatal(failure)
				}
				data := result.(map[string]any)
				item := data["item"].(map[string]string)
				if data["changed"] != tc.changed || item["profile_source"] != tc.source || item["profile_presence"] != tc.presence {
					t.Fatalf("%v", data)
				}
			}
			if tc.config != "" {
				body, err := os.ReadFile(config)
				if err != nil || string(body) != tc.config {
					t.Fatal("existing config changed")
				}
			}
			if tc.config == "" && tc.code != "" {
				if _, err := os.Lstat(config); !os.IsNotExist(err) {
					t.Fatal("failed init published configuration")
				}
			}
			if output.Len() != 0 || diagnostic.Len() != 0 {
				t.Fatal("selection emitted unstructured output")
			}
			if tc.storage == "" {
				if _, err := os.Lstat(filepath.Join(app.isolatedRoot, "data")); !os.IsNotExist(err) {
					t.Fatal("namespace lookup initialized storage")
				}
			}
		})
	}
}

func TestInitInteractionRequiresActualTerminals(t *testing.T) {
	for _, options := range []map[string]string{{"interactive": "true"}, {"interactive": "true", "no-interactive": "true"}} {
		_, failure := New("test", "test").initDirectory(context.Background(), IO{In: forbiddenInitInput{}, Out: &bytes.Buffer{}, Err: &bytes.Buffer{}}, Request{Options: options, OutputFormat: presentation.FormatText}, t.TempDir())
		if failure == nil || failure.Code != "invalid_argument" {
			t.Fatal(failure)
		}
	}
}
