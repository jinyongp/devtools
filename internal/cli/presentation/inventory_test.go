package presentation

import (
	"fmt"
	"strings"
	"testing"

	"github.com/jinyongp/devtools/internal/cliui"
)

func TestProxyInventoryRetainsEveryRouteWithoutRecordExpansion(t *testing.T) {
	var items []any
	for i := 0; i < 20; i++ {
		state := "ready"
		if i >= 13 && i < 19 {
			state = "activation_required"
		}
		if i == 19 {
			state = "alias_missing"
		}
		items = append(items, map[string]any{"kind": "route", "host": fmt.Sprintf("service-%02d.localhost", i), "profile": fmt.Sprintf("app-%d", i%3), "directory": fmt.Sprintf("/work/app-%d", i%3), "target_port": 3000 + i, "listener": "local-https", "status": state, "proxy": fmt.Sprintf("service-%02d", i), "alias": nil, "host_policy": map[string]any{"mode": "rewrite"}})
	}
	for i := 0; i < 3; i++ {
		items = append(items, map[string]any{"kind": "instance", "profile": "app-0", "directory": fmt.Sprintf("/missing/worktree-%d", i), "status": "location_missing"})
	}
	doc := publicDocument(t, "proxy list", map[string]any{"items": items})
	for _, width := range []int{40, 80, 120} {
		out, err := cliui.Render(doc, cliui.Options{Width: width})
		if err != nil {
			t.Fatal(err)
		}
		s := string(out)
		if strings.Count(s, "\n") > 55 || strings.Contains(s, "Item ") || strings.Contains(s, "host_policy") || strings.Contains(s, "null") {
			t.Fatal(width, s)
		}
		for i := 0; i < 20; i++ {
			if strings.Count(s, fmt.Sprintf("service-%02d.localhost", i)) != 1 || !strings.Contains(s, fmt.Sprint(3000+i)) {
				t.Fatal("route lost or duplicated", s)
			}
		}
		for i := 0; i < 3; i++ {
			if strings.Count(s, fmt.Sprintf("/missing/worktree-%d", i)) != 1 {
				t.Fatal("missing location lost", s)
			}
		}
		if !strings.Contains(s, "Needs activation") || !strings.Contains(s, "Alias missing") || strings.Count(s, "Missing locations") != 1 {
			t.Fatal(s)
		}
	}
}

func TestProcessInventorySeparatesStateAndReadiness(t *testing.T) {
	id := "11111111-1111-4111-8111-111111111111"
	doc := publicDocument(t, "process list", map[string]any{"items": []any{map[string]any{"command": "web", "state": "running", "id": id, "profile": "app", "directory": "/work/app", "ready_configured": false, "readiness": nil}}})
	out, err := cliui.Render(doc, cliui.Options{Width: 40})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(out), id) != 1 || !strings.Contains(string(out), "Running") || !strings.Contains(string(out), "Not configured") || strings.Count(string(out), "\n") > 10 {
		t.Fatal(string(out))
	}
}
