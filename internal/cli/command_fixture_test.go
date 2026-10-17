package cli

import (
	"encoding/json"
	"testing"
)

func fixtureExec(t *testing.T, kind string) string {
	t.Helper()
	body, err := json.Marshal(commandFixture(t, kind))
	if err != nil {
		t.Fatal(err)
	}
	return string(body)
}
