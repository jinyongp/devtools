# Process operation locks and recovery

Scope: the v0.36.2 follow-up workstream
`d19b52f2-2393-42b3-b04b-22769305bb89`. This document defines the protocol
implemented by the next unit; it does not claim that the old global serialization
has already been removed.

## Ownership and order

Use existing private-file RW locks, request receipts and inherited execution
leases. No new daemon, journal format or lock framework is needed.

| Lock / identity | Mode and lifetime | Protected state |
| --- | --- | --- |
| `processes/operations.lock` | Shared for each Apply; exclusive for Store.Lock callers | Quiescence barrier for maintenance and old clients |
| Request UUID, including identical-input retries | Exclusive until receipt completion/pending persistence | Fingerprint, saved result and the request's execution identity |
| Profile + canonical project root + command | Exclusive through start/stop/restart and result persistence | Singleton selection and all executions in that singleton |
| `processes/publication.lock` | Exclusive for staging cleanup or atomic launch publication only | Unpublished stages, rename and directory durability |
| Existing execution lease | Held by the authenticated supervisor/launcher | Actual child lifetime and crash reconciliation |
| Existing port registry, vault slot/launch, maintenance gates | Existing owners and scopes | Registry consistency, frozen input and protected storage |

Order is operations barrier -> request -> singleton -> publication. Release
publication before IPC, spawn handoff, old-child stop or startup observation.
The singleton lock also serializes stops of older executions with a concurrent
new start for the same command. Directory is the canonical execution-location
identity already used by the instance registry, including first registration;
do not use a newly allocated instance ID as the initial lock key.

Project lifecycle retains request -> project -> process locks. The automatic
value coordinator enters the same Apply path; it does not acquire a second
process lock while one is held. Resolve existing request receipts before project
configuration so replay still works after files change or disappear. For a
pending start, use its persisted execution identity when one exists. Re-read
mutable execution state after acquiring the singleton lock.

The old `operations.lock` path stays in use. Old binaries take it exclusively,
so mixed old/new processes serialize safely. New binaries share the barrier and
only conflict on the same request or singleton. The writer-intent turnstile in
the existing RW lock stops new readers from starving queued maintenance.

## Maintenance callers and boundaries

| Caller | Preserved rule |
| --- | --- |
| `internal/cleanup/cleanup.go` lock / preview | Exclusive services barrier -> slot -> maintenance. No in-flight receipt, staging or active execution collection. Do not reverse acquisition order. |
| `internal/cli/ports.go` port prune | Exclusive services barrier before checking process ownership and editing registry. |
| `internal/cli/ports.go` instance move/remove | Exclusive barrier; active execution checks remain authoritative. |
| `internal/backup/backup.go` restore | Existing vault launch exclusion and maintenance gate remain; no new process lock under an already-held maintenance gate. |
| `internal/vault/launch.go`, frozen input prepare/serve | Existing slot/launch leases prevent secret delivery during incompatible maintenance. Keep preparation before stopping the old execution. |
| CLI, Dashboard, automatic value restart | All use Store.Apply; no caller may bypass request/singleton serialization. |

Singleton discovery filters stored records by profile/root/command **before**
contacting supervisors. Unrelated instances must not add RPC waits to a start.
Public process list remains a full observation operation.

## Failure and recovery

The request receipt is durable before side effects. Atomic staging publication
and inherited leases keep an admitted execution distinct from an unpublished
stage. Cleanup and publication take the same short lock, so cleanup cannot erase
another request's live stage. Process exit releases OS locks; a later identical
request inspects the existing receipt and execution rather than creating another.

Pre-admission failure leaves the old child alive. After admission, reuse the
request's execution ID. Confirmed pre-spawn failure stores a Done result and
returns `execution_failed` on replay, derived from the immutable record without
changing the receipt format. Pending/unknown outcomes retain inspection/recovery
behavior, never a guessed success, replacement or PID-based kill. A crash after
old-child termination preserves the confirmed outage.

Cancellation while waiting for a lock returns the existing context diagnostic.
Cancellation during an operation persists the observed partial result through
the existing receipt path. Do not release singleton ownership early merely to
make a timing test pass. Unix TERM and Windows CTRL_BREAK retain their three
second grace periods and authenticated tree/lease cleanup.

## Required checks

- Independent singleton B completes while A's authenticated stop is held by a
  test marker; B completion must precede release of A. Wall-clock limits only
  bound hangs, not establish concurrency correctness.
- Same request, conflicting fingerprints and distinct requests for one singleton
  remain serialized. A canceled contender causes no side effects.
- Exclusive Store.Lock waits for active operations and blocks new Apply calls;
  ports/instance/cleanup retain their existing safety checks.
- Staging publication and cleanup serialize across independent requests; crash
  recovery, inherited lease, frozen restart and late-outage tests still pass.
- Manual/automatic restart, readiness retry, vault lock/rewrap, restore/cleanup,
  and installed Linux/macOS/Windows race/native lanes cover the affected boundary.
- Repeat the same isolated TERM-marker workload at least three times before and
  after; retain raw durations, source SHA and binary digest. v0.36.2 baseline
  medians were 0.103 seconds alone and 3.099 seconds under contention on this host.
  No general performance threshold is inferred from those samples.
