package vault

import (
	"context"
	"github.com/jinyongp/devtools/internal/maintenance"
)

// BeginDelivery validates the original ticket while retaining the physical
// launch barrier. External uncertainty never creates an own-process journal.
// The caller bounds ctx by the prepared input deadline and always releases it.
func (c *SessionClient) BeginDelivery(ctx context.Context, ticket LaunchTicket) (func(), error) {
	if !validLaunchTicket(ticket) || ticket.VaultID != c.Endpoint.VaultID || ticket.SessionID != c.Endpoint.SessionID {
		return nil, ErrInputChanged
	}
	release, err := maintenance.LockShared(ctx, launchGate(c.Directory))
	if err != nil {
		return nil, err
	}
	if _, err = c.requestWithTicket(ctx, "admit", nil, false, &ticket); err != nil {
		release()
		return nil, err
	}
	return release, nil
}
