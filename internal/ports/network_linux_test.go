//go:build linux

package ports

import (
	"reflect"
	"testing"
)

func TestLinuxAvailabilityProbesDoNotEnumerateInterfaces(t *testing.T) {
	got, err := availabilityProbeAddresses()
	if err != nil {
		t.Fatal(err)
	}
	want := []probeAddress{{"tcp4", "0.0.0.0"}, {"tcp6", "::"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Linux probe targets = %#v, want %#v", got, want)
	}
}
