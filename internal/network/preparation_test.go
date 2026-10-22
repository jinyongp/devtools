package network

import "testing"

func TestPreparationOptions(t *testing.T) {
	truth, falsity := true, false
	for _, tc := range []struct {
		p, n *bool
		want PreparationMode
	}{
		{nil, nil, PrepareAuto}, {&truth, nil, PrepareOn}, {&falsity, nil, PrepareOff},
		{nil, &truth, PrepareOff}, {nil, &falsity, PrepareAuto},
	} {
		got, e := PreparationOptions(tc.p, tc.n)
		if e != nil || got != tc.want {
			t.Fatalf("got %q %v, want %q", got, e, tc.want)
		}
	}
	for _, p := range []*bool{&truth, &falsity} {
		for _, n := range []*bool{&truth, &falsity} {
			if _, e := PreparationOptions(p, n); e == nil || e.Code != "invalid_argument" {
				t.Fatal(e)
			}
		}
	}
}
