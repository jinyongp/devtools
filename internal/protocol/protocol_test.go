package protocol

import (
	"bytes"
	"encoding/json"
	"io"
	"testing"
)

func TestSuccessAlwaysIncludesData(t *testing.T) {
	for _, data := range []any{nil, false, 0, "", []string{}, map[string]any{}} {
		var output bytes.Buffer
		if err := Success(&output, data); err != nil {
			t.Fatal(err)
		}
		decoder := json.NewDecoder(&output)
		var response map[string]json.RawMessage
		if err := decoder.Decode(&response); err != nil {
			t.Fatal(err)
		}
		want, err := json.Marshal(data)
		var version int
		versionErr := json.Unmarshal(response["schema_version"], &version)
		if err != nil || versionErr != nil || version != EnvelopeVersion || !bytes.Equal(response["data"], want) || string(response["ok"]) != "true" {
			t.Fatalf("lost success payload: %#v", response)
		}
		if _, exists := response["error"]; exists || len(response) != 3 {
			t.Fatalf("unexpected success fields: %#v", response)
		}
		if err := decoder.Decode(new(any)); err != io.EOF {
			t.Fatalf("more than one response: %v", err)
		}
	}
}

func TestFailureNeverIncludesSuccessData(t *testing.T) {
	var output bytes.Buffer
	if err := Failure(&output, NewError("not_found", "Not found.", 3, nil)); err != nil {
		t.Fatal(err)
	}
	var response map[string]json.RawMessage
	if err := json.Unmarshal(output.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	var version int
	if err := json.Unmarshal(response["schema_version"], &version); err != nil || version != EnvelopeVersion || string(response["ok"]) != "false" || response["error"] == nil || len(response) != 3 {
		t.Fatalf("unexpected failure fields: %s", output.String())
	}
	if _, exists := response["data"]; exists {
		t.Fatal("failure contains data")
	}
}
