package cli

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestExecutionControlsRejectAmbiguousOrSecretBodies(t *testing.T) {
	a := testApp(t)
	id := "01234567-89ab-4cde-8fab-0123456789ab"
	for _, body := range []string{
		`{"external_execution_id":"job","selection":{}}`,
		`{"external_execution_id":"job","selection":{"keys":null}}`,
		`{"external_execution_id":"job","selection":{"keys":["TOKEN","TOKEN"]}}`,
		`{"external_execution_id":"job","selection":{"keys":[]},"value":"forbidden"}`,
		`{"external_execution_id":"job","external_execution_id":"other","selection":{"keys":[]}}`,
		`{"external_execution_id":"job","selection":{"keys":[]}} {}`,
		`{"external_execution_id":"job","selection":{"keys":[]},"env":null}`,
		`{"external_execution_id":"job","selection":{"keys":[],"bundle":""}}`,
		`{"external_execution_id":"job","selection":{"keys":[null]}}`,
	} {
		code, _, diagnostic := invoke(t, a, body, "values", "execution", "prepare", "--stdin", "--request-id", id, "--profile", "app")
		if code != 2 || !strings.Contains(diagnostic, "invalid_argument") {
			t.Fatal("invalid control body accepted", code, diagnostic)
		}
	}
	for _, flags := range [][]string{{}, {"--file", "not-read", "--stdin"}, {"--stdin=false"}} {
		input := &observedSecretReader{}
		var out, diagnostic bytes.Buffer
		args := append([]string{"values", "execution", "prepare", "--request-id", id, "--profile", "app"}, flags...)
		if code := a.Run(context.Background(), args, IO{In: input, Out: &out, Err: &diagnostic}); code != 2 || input.read {
			t.Fatal("invalid source read input", code)
		}
	}
}
