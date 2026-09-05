package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"
)

func TestInitCommand(t *testing.T) {
	t.Chdir(t.TempDir())
	app := New("test", "abc")
	for _, expected := range []bool{true, false} {
		var out, diagnostic bytes.Buffer
		code := app.Run(context.Background(), []string{"init", "--profile", "myapp"}, IO{Out: &out, Err: &diagnostic})
		if code != 0 || diagnostic.Len() != 0 {
			t.Fatalf("init: %d %s", code, &diagnostic)
		}
		var response struct {
			Data struct {
				Created bool   `json:"created"`
				Profile string `json:"profile"`
			} `json:"data"`
		}
		if err := json.Unmarshal(out.Bytes(), &response); err != nil {
			t.Fatal(err)
		}
		if response.Data.Created != expected || response.Data.Profile != "myapp" {
			t.Fatalf("response: %+v", response)
		}
	}
}
