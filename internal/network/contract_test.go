package network

import "testing"

func TestRouteURLsPreserveSchemeAndPort(t *testing.T) {
	for _, tc := range []struct {
		http, https int
		want        string
	}{
		{20200, 0, "http://app.localhost:20200"}, {80, 0, "http://app.localhost"},
		{20200, 443, "https://app.localhost"}, {18080, 18443, "https://app.localhost:18443"},
	} {
		got, err := RouteURL("app.localhost", tc.http, tc.https)
		if err != nil || got != tc.want {
			t.Fatalf("%+v: %q %v", tc, got, err)
		}
	}
	for _, bad := range []string{"app.localhost:3000", "https://app.test", "APP.test", "-bad.test", "a..test", "127.0.0.1"} {
		if _, err := RouteURL(bad, 80, 443); err == nil {
			t.Fatalf("accepted %q", bad)
		}
	}
}

func TestRouteIdentityDoesNotDependOnGlobalDisplayName(t *testing.T) {
	project := RouteRef{Scope: "project", Profile: "app", InstanceID: "0123456789abcdef0123456789abcdef", Proxy: "web"}
	first := RouteRef{Scope: "global", RouteID: "11111111-1111-4111-8111-111111111111"}
	second := RouteRef{Scope: "global", RouteID: "22222222-2222-4222-8222-222222222222"}
	if !project.Valid() || !first.Valid() || !second.Valid() || first.Key() == second.Key() || first.Key() == project.Key() {
		t.Fatal("identities overlap")
	}
	first.Profile = "app"
	if first.Valid() {
		t.Fatal("global route accepted fake project identity")
	}
}

func TestDisabledTombstoneCannotContainAnActiveSnapshot(t *testing.T) {
	a := Activation{Route: RouteRef{Scope: "global", RouteID: "11111111-1111-4111-8111-111111111111"}, Mode: "disabled"}
	if !a.Valid() {
		t.Fatal("disabled tombstone rejected")
	}
	a.Host = "app.test"
	if a.Valid() {
		t.Fatal("disabled snapshot retained active host")
	}
}

func TestSocketAddressRequiresExplicitIPAndNonzeroPort(t *testing.T) {
	for _, a := range []string{"127.0.0.1:18443", "[::1]:18443", "0.0.0.0:443"} {
		if _, _, err := ParseAddress(a); err != nil {
			t.Fatal(a, err)
		}
	}
	for _, a := range []string{"localhost:443", "127.0.0.1:0", "127.0.0.1:65536"} {
		if _, _, err := ParseAddress(a); err == nil {
			t.Fatal("accepted", a)
		}
	}
}
