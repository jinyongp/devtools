package network

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestHostPolicyOptionalStorageAndStrictLegacyReader(t *testing.T) {
	// This projection is the pre-policy global route storage contract.
	type legacyRoute struct {
		ID, Name, Host, Reservation, Listener, DNS string
	}
	for _, policy := range []HostPolicy{"", HostPreserve} {
		body, err := json.Marshal(GlobalRoute{HostPolicy: policy})
		if err != nil {
			t.Fatal(err)
		}
		decoder := json.NewDecoder(strings.NewReader(string(body)))
		decoder.DisallowUnknownFields()
		var old legacyRoute
		err = decoder.Decode(&old)
		if (err != nil) != (policy == HostPreserve) {
			t.Fatalf("legacy read of %s: %v", body, err)
		}
		var current GlobalRoute
		if err := json.Unmarshal(body, &current); err != nil || current.HostPolicy != policy {
			t.Fatal("current roundtrip", current, err)
		}
	}
}
