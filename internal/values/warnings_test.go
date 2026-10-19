package values

import "testing"

func TestFrontendWarningsIdentifyNamesWithoutChangingTheirKinds(t *testing.T) {
	items := []ImportItem{{Key: "VITE_TOKEN", Kind: Secret, Action: "add"}, {Key: "NEXT_PUBLIC_URL", Kind: Variable, Action: "add"}, {Key: "TOKEN", Kind: Secret, Action: "add"}}
	warnings := ImportWarnings(items)
	if len(warnings) != 1 || warnings[0].Code != "frontend_public_prefix" || warnings[0].Key != "VITE_TOKEN" {
		t.Fatal("frontend name warning missing or public value warned")
	}
	if items[0].Kind != Secret {
		t.Fatal("warning reclassified a secret")
	}
	if FrontendSecretWarning("PRIVATE_TOKEN") != nil {
		t.Fatal("ordinary secret warned")
	}
	prefixes := FrontendPublicPrefixes()
	prefixes[0] = "mutated"
	if FrontendSecretWarning("VITE_TOKEN") == nil {
		t.Fatal("caller changed prefix policy")
	}
}
