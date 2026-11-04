package services

import (
	"context"
	"sort"
	"strings"
	"time"

	"github.com/jinyongp/devtools/internal/completion"
	"github.com/jinyongp/devtools/internal/protocol"
	"github.com/jinyongp/devtools/internal/retention"
)

type CompletionSelection struct {
	NavigationSelection
	Action, Prefix string
	ActiveOnly     bool
}

// CompletionRecords observes only the capabilities needed by the requested
// operation. It never runs probes, renews values, or writes execution records.
func (s Store) CompletionRecords(ctx context.Context, selection CompletionSelection) ([]Record, *protocol.Error) {
	records, condition := s.StoredMetadata()
	if condition != nil {
		return nil, condition
	}
	items, pending := []Record{}, []Record{}
	allows := func(record Record) bool {
		switch selection.Action {
		case "stop":
			return record.Control.Stop
		case "restart":
			return record.Control.Restart
		case "check", "wait":
			return record.Control.Check
		case "logs":
			return record.Capture && (record.EndedAt == nil || time.Since(*record.EndedAt) <= retention.RawProcessLogAge)
		case "status":
			return true
		}
		return false
	}
	for _, record := range records {
		if ctx.Err() != nil {
			break
		}
		if record.DetailRetired || !strings.HasPrefix(record.ID, selection.Prefix) || !selectedNavigation(selection.NavigationSelection, record.Profile, record.Directory, record.Instance, record.Command, record.Env) {
			continue
		}
		if selection.ActiveOnly && (record.EndedAt != nil || record.State == "interrupted") {
			continue
		}
		if selection.Action == "status" || selection.Action == "logs" || record.EndedAt != nil {
			public := record.publicContract()
			if allows(public) {
				items = append(items, public)
			}
			continue
		}
		if (selection.Action == "check" || selection.Action == "wait") && !record.ReadyConfigured {
			continue
		}
		pending = append(pending, record)
	}
	// A slow controller must not hide healthy siblings or exceed the shell's
	// 200ms total budget. Reuse authenticated status RPC, never saved authority.
	items = append(items, completion.Observe(ctx, pending, func(ctx context.Context, record Record) (Record, bool) {
		live, err := s.rpc(ctx, record.ID, "status")
		live = live.publicContract()
		return live, err == nil && allows(live)
	})...)
	sort.Slice(items, func(i, j int) bool { return items[i].ID < items[j].ID })
	return items, nil
}
