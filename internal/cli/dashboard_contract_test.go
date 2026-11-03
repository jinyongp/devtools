package cli

import (
	"context"
	"path/filepath"
	"testing"
)

func TestDashboardCollectionUsesRegistryAndOwnedStore(t *testing.T) {
	app := testApp(t)
	bridge := app.DashboardContracts()
	root := privateTempDir(t)
	cache := filepath.Join(privateTempDir(t), "queries")
	options := map[string][]string{"profile": {"example"}, "view": {"history"}, "limit": {"1"}, "state": {"running", "unknown"}}
	result, condition := bridge.Collection(context.Background(), "process list", root, cache, options)
	if condition != nil || result["selection"] == nil || result["items"] == nil {
		t.Fatal(result, condition)
	}
	if _, condition := bridge.Collection(context.Background(), "process stop", root, cache, nil); condition == nil || condition.Code != "invalid_argument" {
		t.Fatal("mutation passed collection boundary", condition)
	}
	options["query-cache-dir"] = []string{"/unrelated"}
	if _, condition := bridge.Collection(context.Background(), "process list", root, cache, options); condition == nil || condition.Code != "invalid_argument" {
		t.Fatal("HTTP input replaced the server cache", condition)
	}
}
