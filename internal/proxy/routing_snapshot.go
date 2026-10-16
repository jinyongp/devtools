package proxy

import (
	"context"

	"github.com/jinyongp/devtools/internal/protocol"
)

type routeKey struct{ listener, host string }

type routeDecision struct {
	item    Item
	matches int
}

// routingSnapshot is published with the resolved management list. It is never
// changed after publication; freshness checks still precede every acquisition.
type routingSnapshot struct{ routes map[routeKey]routeDecision }

func newRoutingSnapshot(items []Item) *routingSnapshot {
	snapshot := &routingSnapshot{routes: map[routeKey]routeDecision{}}
	for _, item := range items {
		if item.Kind != "route" || item.Host == nil {
			continue
		}
		listener := item.Listener
		if listener == "" {
			listener = "default"
		}
		// The empty listener is the legacy handler's all-listener lookup.
		for _, key := range []routeKey{{listener, *item.Host}, {"", *item.Host}} {
			decision := snapshot.routes[key]
			decision.matches++
			decision.item = item
			snapshot.routes[key] = decision
		}
	}
	return snapshot
}

type routeLookup interface {
	lookupRoute(context.Context, string, string) (routeDecision, *protocol.Error)
}

func (r Resolver) lookupRoute(ctx context.Context, listener, host string) (routeDecision, *protocol.Error) {
	identity := routeSnapshot{routingOnly: true}
	if _, err := r.list(ctx, "", &identity); err != nil {
		return routeDecision{}, err
	}
	return identity.routing.routes[routeKey{listener, host}], nil
}

func selectRoute(ctx context.Context, routes RouteLister, listener, host string) (routeDecision, *protocol.Error) {
	if lookup, ok := routes.(routeLookup); ok {
		return lookup.lookupRoute(ctx, listener, host)
	}
	items, err := routes.List(ctx, "")
	if err != nil {
		return routeDecision{}, err
	}
	return newRoutingSnapshot(items).routes[routeKey{listener, host}], nil
}
