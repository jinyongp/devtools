package configcontract

import (
	"encoding/json"
	"os"
	"testing"
)

// These expectations were authored with the contract, independently of this provider.
func TestPublishedCases(t *testing.T) {
	data, e := os.ReadFile("cases.json")
	if e != nil {
		t.Fatal(e)
	}
	var suite struct {
		Transport []struct {
			Raw   string `json:"raw"`
			Valid bool   `json:"valid"`
		} `json:"transport"`
		Cases []struct {
			Schema    string `json:"schema"`
			Valid     bool   `json:"valid"`
			Value     any    `json:"value"`
			Raw       string `json:"raw"`
			Transport string `json:"transport"`
		}
	}
	if e = json.Unmarshal(data, &suite); e != nil {
		t.Fatal(e)
	}
	for i, c := range suite.Cases {
		if c.Schema == "" {
			continue
		}
		got := len(Validate(c.Schema, c.Value)) == 0
		if got != c.Valid {
			t.Errorf("case %d %s: valid=%v, want %v", i, c.Schema, got, c.Valid)
		}
	}
	for i, c := range suite.Transport {
		_, e := Decode([]byte(c.Raw))
		if (e == nil) != c.Valid {
			t.Errorf("transport case %d: %v", i, e)
		}
	}
}
func TestStrictInput(t *testing.T) {
	for _, data := range []string{`{"x":1,"x":2}`, `{"x":9223372036854775808}`, `{"x":1.0}`, `{"x":1e2}`, `{"x":"\ud800"}`, `{} {}`, "\xef\xbb\xbf{}"} {
		if _, e := Decode([]byte(data)); e == nil {
			t.Errorf("accepted %s", data)
		}
	}
	if _, e := Decode([]byte(`{"x":"\ud83d\ude00","n":9223372036854775807}`)); e != nil {
		t.Fatal(e)
	}
}
