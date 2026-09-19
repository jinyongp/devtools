package cli

import (
	"encoding/json"
	"testing"
)

func decodeCLIData(t *testing.T, output string) map[string]any {
	t.Helper()
	var envelope map[string]any
	if err := json.Unmarshal([]byte(output), &envelope); err != nil {
		t.Fatal(err)
	}
	data, ok := envelope["data"].(map[string]any)
	if !ok {
		t.Fatalf("data = %#v", envelope["data"])
	}
	return data
}
