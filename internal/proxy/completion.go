package proxy

import (
	"context"
	"strings"

	"github.com/jinyongp/devtools/internal/completion"
	"github.com/jinyongp/devtools/internal/expose"
	"github.com/jinyongp/devtools/internal/network"
	"github.com/jinyongp/devtools/internal/protocol"
)

func (m Manager) CompletionListeners(ctx context.Context, stopping bool, prefix string) ([]string, *protocol.Error) {
	state, condition := m.portStore().Read()
	if condition != nil {
		return nil, condition
	}
	names := []string{}
	if state.Network != nil {
		for _, listener := range state.Network.Listeners {
			if strings.HasPrefix(listener.Name, prefix) {
				names = append(names, listener.Name)
			}
		}
	}
	if len(names) == 0 && state.Network == nil && strings.HasPrefix(network.DefaultListener, prefix) {
		names = append(names, network.DefaultListener)
	}
	if !stopping {
		return names, nil
	}
	return completion.Observe(ctx, names, func(ctx context.Context, name string) (string, bool) {
		if state.Listener(name) == nil {
			status, issue := m.Status(ctx)
			return name, issue == nil && status.Running
		}
		status, issue := m.NetworkStatus(ctx, name)
		return name, issue == nil && status["running"] == true && status["daemon_running"] == true
	}), nil
}

func (m Manager) CompletionExposures(ctx context.Context, stopping bool, prefix string) ([]expose.Public, *protocol.Error) {
	records, condition := (expose.Store{Data: m.Data}).List()
	if condition != nil {
		return nil, condition
	}
	items := []expose.Public{}
	for _, record := range records {
		if strings.HasPrefix(record.ID, prefix) && (!stopping || record.State == "active" || record.State == "starting") {
			items = append(items, record.Public)
		}
	}
	if !stopping {
		return items, nil
	}
	return completion.Observe(ctx, items, func(ctx context.Context, item expose.Public) (expose.Public, bool) {
		live, issue := m.ExposureStatus(ctx, item.ID)
		return live, issue == nil && (live.State == "active" || live.State == "starting")
	}), nil
}
