package maintenance

import "context"

// AcquireForRecovery acquires only the private exclusive gate. An encrypted
// forward journal cannot be recovered by the ordinary before-image reader, and
// its caller must first obtain keys outside this gate. The caller must inspect
// and authenticate the pending state before permitting any domain access.
// Normal reads/writes must continue using AcquireShared/AcquireExclusive.
func AcquireForRecovery(ctx context.Context, root string) (func(), error) {
	return acquireMaintenanceGate(ctx, root, lockExclusive)
}
