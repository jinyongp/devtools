package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestKnownOperationFailuresExplainTheirConditions(t *testing.T) {
	app := testApp(t)
	path := filepath.Join(privateTempDir(t), "must-not-exist")
	for _, tc := range []struct {
		args []string
		exit int
		want string
	}{
		{[]string{"process", "status", "00000000-0000-4000-8000-000000000001"}, 3, "No execution with this ID"},
		{[]string{"proxy", "up", "missing", "--global", "--request-id", "00000000-0000-4000-8000-000000000003"}, 3, "selected route does not exist"},
		{[]string{"proxy", "configure", "--https-address", "127.0.0.1:9443", "--disable-https", "--request-id", "00000000-0000-4000-8000-000000000002"}, 2, "--https-address or --disable-https"},
		{[]string{"backup", "keygen", "--identity-file", path, "--recipient-file", path}, 2, "output paths must be different"},
	} {
		for _, format := range []string{"json", "text"} {
			args := append([]string{"--format", format}, tc.args...)
			code, out, diagnostic := invoke(t, app, "", args...)
			if code != tc.exit || out != "" || !strings.Contains(strings.Join(strings.Fields(diagnostic), " "), tc.want) {
				t.Fatalf("%v: %d %s %s", args, code, out, diagnostic)
			}
		}
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("failed keygen created an output", err)
	}
}
