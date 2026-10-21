package valuehandoff

import (
	"context"
	"sync"
	"time"

	"github.com/jinyongp/devtools/internal/vault"
)

// ServeRunner is hosted by the caller-owned process, never by a devtools daemon.
// The reference receiver currently accepts registration probes only; delivery
// handling is installed by the delivery/log-protection work units.
func ServeRunner(ctx context.Context, root, runner, instance string) error {
	address, err := Address(root, runner, instance)
	if err != nil {
		return err
	}
	listener, err := vault.ListenPeer(ctx, root, runner, instance)
	if err != nil {
		return err
	}
	defer listener.Close()
	var wg sync.WaitGroup
	defer wg.Wait()
	stop := context.AfterFunc(ctx, func() { listener.Close() })
	defer stop()
	capacity := make(chan struct{}, 8)
	for {
		select {
		case capacity <- struct{}{}:
		case <-ctx.Done():
			return ctx.Err()
		}
		c, err := listener.Accept(ctx)
		if err != nil {
			<-capacity
			return err
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer func() { <-capacity }()
			defer c.Close()
			c.SetDeadline(time.Now().Add(5 * time.Second))
			stop := context.AfterFunc(ctx, func() { c.Close() })
			defer stop()
			server, err := vault.NewID()
			if err != nil {
				return
			}
			h := helloFrame{1, "hello", address.RootID, address.VaultID, runner, instance, server}
			if writeFrame(c, h, maxControlFrame) != nil {
				return
			}
			var probe probeFrame
			if readControl(c, &probe) != nil || probe.Protocol != 1 || probe.Type != "probe" || probe.RootID != address.RootID || probe.VaultID != address.VaultID || probe.RunnerID != runner || probe.InstanceID != instance || probe.ServerChallenge != server || !idRE.MatchString(probe.ClientChallenge) {
				return
			}
			_ = writeFrame(c, readyFrame{1, "registered", server, probe.ClientChallenge, 1}, maxControlFrame)
		}()
	}
}
