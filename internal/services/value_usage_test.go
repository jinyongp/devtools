package services

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jinyongp/devtools/internal/execution"
	"github.com/jinyongp/devtools/internal/project"
	"github.com/jinyongp/devtools/internal/protocol"
	"github.com/jinyongp/devtools/internal/tasks"
	"github.com/jinyongp/devtools/internal/values"
)

func TestValueUsesSeparatesDeclarationsDeliveriesAndUnknown(t *testing.T) {
	ctx := context.Background()
	data, err := filepath.EvalSymlinks(privateTempDir(t))
	if err != nil {
		t.Fatal(err)
	}
	directory := privateTempDir(t)
	config := filepath.Join(directory, "devtools.toml")
	writeConfig := func(bundle string) {
		t.Helper()
		if err := os.WriteFile(config, []byte("profile = \"app\"\nbundle = \""+bundle+"\"\n[commands.web]\nexec = [\"fixture\"]\ninject = true\n"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	writeConfig("frontend")
	store := values.ModernStore{Directory: filepath.Join(data, "profiles"), Profile: "app"}
	set := func(name, value string) {
		t.Helper()
		if _, e := store.Update(ctx, func(s *values.Editor) (bool, *protocol.Error) { return s.Set(values.Variable, name, "", value) }); e != nil {
			t.Fatal(e)
		}
	}
	set("URL", "synthetic-delivery-before")
	set("OTHER", "synthetic-unselected-before")
	view, e := store.Inspect(ctx, "")
	if e != nil {
		t.Fatal(e)
	}
	if _, e := store.Apply(ctx, values.Change{Action: "bundle.set", Revision: view.Revision, RequestID: tasks.ID(), Bundle: &values.BundleChange{Name: "frontend", Members: []string{"URL"}}}); e != nil {
		t.Fatal(e)
	}
	p, e := project.Resolve(directory, "")
	if e != nil {
		t.Fatal(e)
	}
	command, e := execution.ResolveConfigured(p, "web", nil, nil)
	if e != nil {
		t.Fatal(e)
	}
	snapshot, e := values.CapturePublic(ctx, data, "app", "", true, time.Now(), "frontend")
	if e != nil {
		t.Fatal(e)
	}
	defer snapshot.Destroy()
	delivery, err := values.NewInputVersion(snapshot.Metadata(), "", command.ValueInputSpec())
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	record := Record{ID: tasks.ID(), Profile: "app", Directory: p.Root, Command: "web", State: "exited", CreatedAt: now, StartedAt: &now, EndedAt: &now, Selection: "frontend", InputVersion: delivery}
	service := Store{Data: data}
	if e := writePrivate(service.path(record.ID, "record.json"), record); e != nil {
		t.Fatal(e)
	}
	items, e := service.ValueUses(ctx, p)
	if e != nil || len(items) != 2 || items[0].Kind != "declared" || items[0].Values.Delivered != nil || items[1].Kind != "delivered" || items[1].Values.State != "current" {
		t.Fatal("declaration/delivery distinction", items, e)
	}
	set("OTHER", "synthetic-unselected-after")
	if state := service.InspectRecordValues(ctx, record).State; state != "current" {
		t.Fatal("unselected change notified", state)
	}
	writeConfig("none")
	if state := service.InspectRecordValues(ctx, record).State; state != "pending" {
		t.Fatal("new declared selection not compared", state)
	}
	explicit := "frontend"
	record.BundleOverride = &explicit
	if state := service.InspectRecordValues(ctx, record).State; state != "current" {
		t.Fatal("explicit override was ignored", state)
	}
	set("URL", "synthetic-delivery-after")
	if state := service.InspectRecordValues(ctx, record).State; state != "pending" {
		t.Fatal("changed input not pending", state)
	}
	record.State = "unknown"
	if state := service.InspectRecordValues(ctx, record).State; state != "unknown" {
		t.Fatal("uncertain process delivery promoted", state)
	}
	record.State = "exited"
	record.InputVersion = nil
	if state := service.InspectRecordValues(ctx, record).State; state != "unknown" {
		t.Fatal("legacy delivery manufactured", state)
	}
	record.StartedAt = nil
	if state := service.InspectRecordValues(ctx, record).State; state != "unknown" {
		t.Fatal("preparation promoted to delivery", state)
	}
}
