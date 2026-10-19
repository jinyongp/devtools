package vault

import (
	"bytes"
	"encoding/json"
	"fmt"
	"testing"
)

func TestSensitiveMaterialDiagnosticBoundary(t *testing.T) {
	input := []byte("synthetic-sensitive-fixture")
	m := NewMaterial(input)
	input[0] = '!'
	if got := m.CopyBytes(); string(got) != "synthetic-sensitive-fixture" {
		t.Fatal("material does not own its input")
	}
	for _, format := range []string{"%v", "%+v", "%#v", "%s", "%x", "%q"} {
		for _, value := range []any{m, *m, struct{ M *Material }{m}} {
			if got := fmt.Sprintf(format, value); bytes.Contains([]byte(got), []byte("synthetic-sensitive")) {
				t.Fatal("sensitive diagnostic serialization")
			}
		}
	}
	if b, err := json.Marshal(struct{ M *Material }{m}); err == nil || bytes.Contains(b, []byte("synthetic-sensitive")) {
		t.Fatal("sensitive JSON serialization")
	}
	m.Destroy()
	if m.CopyBytes() != nil {
		t.Fatal("destroyed material retained")
	}
}
