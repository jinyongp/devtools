package cli

import (
	"strings"
	"testing"
)

func TestRunnerControlBodiesRemainClosedAndRootScoped(t *testing.T) {
	a := testApp(t)
	id := "01234567-89ab-4cde-8fab-0123456789ab"
	for _, body := range []string{`{"runner_id":"x","instance_id":"y","protocol":1,"redaction_protocol":1}`, `{"runner_id":"01234567-89ab-4cde-8fab-0123456789ab","instance_id":"01234567-89ab-4cde-8fab-0123456789ac","protocol":1}`, `{"runner_id":"01234567-89ab-4cde-8fab-0123456789ab","instance_id":"01234567-89ab-4cde-8fab-0123456789ac","protocol":null,"redaction_protocol":1}`, `{"runner_id":"01234567-89ab-4cde-8fab-0123456789ab","instance_id":"01234567-89ab-4cde-8fab-0123456789ac","protocol":1,"redaction_protocol":1,"endpoint":"tcp://forbidden"}`} {
		code, _, diagnostic := invoke(t, a, body, "values", "runner", "register", "--stdin", "--request-id", id)
		if code != 2 || !strings.Contains(diagnostic, "invalid_argument") {
			t.Fatal("invalid registration accepted", code)
		}
	}
	if code, _, _ := invoke(t, a, "", "values", "runner", "register", "--stdin", "--request-id", id, "--profile", "unrelated"); code != 2 {
		t.Fatal("root-wide registration accepted an ignored profile")
	}
}
