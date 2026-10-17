package dashboard

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jinyongp/devtools/internal/tasks"
)

func TestMain(m *testing.M) {
	if len(os.Args) == 4 && os.Args[1] == "__dashboard-serve" {
		if Serve(context.Background(), os.Args[2], os.Args[3]) != nil {
			os.Exit(90)
		}
		os.Exit(0)
	}
	os.Exit(m.Run())
}
func TestWindowsDetachedDashboardLifecycleAndAuthorization(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cache := filepath.Join(t.TempDir(), "cache")
	data := filepath.Join(t.TempDir(), "data")
	first, failure := Manage(ctx, "start", cache, data, "test")
	if failure != nil {
		t.Fatal(failure)
	}
	t.Cleanup(func() { Manage(context.Background(), "stop", cache, data, "test") })
	second, failure := Manage(ctx, "start", cache, data, "test")
	if failure != nil || first["server_id"] != second["server_id"] {
		t.Fatalf("singleton: %v %v %v", first, second, failure)
	}
	status, failure := Status(ctx, cache)
	if failure != nil || status["running"] != true {
		t.Fatalf("status: %v %v", status, failure)
	}
	var r Registry
	if err := tasks.ReadPrivate(filepath.Join(cache, "dashboard", "server.json"), &r); err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"token", "origin", "host"} {
		req, _ := http.NewRequestWithContext(ctx, "POST", r.Address+"/admin", nil)
		req.Header.Set("Authorization", "Bearer "+r.Token)
		switch kind {
		case "token":
			req.Header.Set("Authorization", "Bearer wrong")
		case "origin":
			req.Header.Set("Origin", "http://outside.invalid")
		case "host":
			req.Host = "outside.invalid"
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != 403 {
			t.Fatalf("%s authorization=%d", kind, resp.StatusCode)
		}
	}
	if _, failure = Manage(ctx, "stop", cache, data, "test"); failure != nil {
		t.Fatal(failure)
	}
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		s, e := Status(ctx, cache)
		if e == nil && s["running"] == false {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("dashboard survived stop")
}
