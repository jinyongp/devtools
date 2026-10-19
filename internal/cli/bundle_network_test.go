package cli

import (
	"context"
	"github.com/jinyongp/devtools/internal/project"
	"github.com/jinyongp/devtools/internal/tasks"
	"testing"
)

func TestProxyPreparationRetryCannotChangeBundleSelection(t *testing.T) {
	app := testApp(t)
	p := project.Context{Root: privateTempDir(t), Profile: "app", Commands: map[string]project.Command{}}
	id := tasks.ID()
	// Even a preparation that failed before side effects owns its UUID. It
	// cannot be resumed with a broader bundle while retaining the old journal.
	_, e := app.prepareProxyNetwork(context.Background(), p, []string{"missing"}, nil, nil, IO{}, id, map[string]string{"missing": "frontend"})
	if e == nil || e.Code != "command_not_found" {
		t.Fatal("expected bounded preparation failure", e)
	}
	_, e = app.prepareProxyNetwork(context.Background(), p, []string{"missing"}, nil, nil, IO{}, id, map[string]string{"missing": "none"})
	if e == nil || e.Code != "request_conflict" {
		t.Fatal("bundle omitted from retry fingerprint", e)
	}
}
