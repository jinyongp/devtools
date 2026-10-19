package vault

import (
	"errors"
	"testing"
)

func TestStrictStructFieldsRejectCaseAliasesButMapsKeepCase(t *testing.T) {
	for _, b := range []string{`{"protocol":1,"Protocol":2}`, `{"Protocol":1}`} {
		var e SessionEndpoint
		if err := StrictDecode([]byte(b), &e); !errors.Is(err, ErrInvalid) {
			t.Fatal("case alias accepted")
		}
	}
	var values map[string]string
	if err := StrictDecode([]byte(`{"Token":"first","TOKEN":"second"}`), &values); err != nil || len(values) != 2 {
		t.Fatal("dynamic map keys lost case distinction", err)
	}
}
