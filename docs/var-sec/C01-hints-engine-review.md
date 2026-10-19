# C01 commit hints and reconciliation engine

Prerequisite unit only; C01 remains active until managed supervisor consumers and
public status integration are implemented and reviewed. Automatic restart is C02.

## Contract and architecture

One bounded `.maintenance/values-change.json` contains only profile name, opaque
profile/storage identities, logical revision and key epoch. It joins the same
ordinary value transaction as profile/catalog/receipt publication, for public and
protected CRUD and bundle/import mutations. It is deliberately unsigned advisory
metadata, never a launch authority or a trusted revision cursor. Restore/migration
can bypass it; periodic authentication must still observe the new generation.
The narrow transaction path exception accepts only this exact filename.

WatchInputChanges inspects immediately on startup, coalesces changed hints for
500ms of quiet (maximum 5s of continuous hints), and reconciles every two seconds
without hints. Duplicate hints do not postpone work. Out-of-order hints ask for
current storage; they never roll it back. Only safe InputObservation metadata is
retained. A one-item mailbox replaces old goals for slow consumers, with owned
copies. No raw values, full decrypted profiles, keys or launch tickets are cached.

The managed caller will own a watcher within its existing supervisor lifecycle,
with compact status metadata. C02 can use a separate detached single-operation
coordinator when an explicit restart policy requires replacing that supervisor;
secret storage/key brokers do not execute application commands. A new persistent
per-root orchestration daemon is unnecessary for this contract.

## Review coverage and corrections

- Reviewed public/protected publication paths, receipt/import atomicity, root WAL
  path validation, bounded private hint reading, malformed/missing hints, lifecycle
  cancellation, timer scheduling, copy ownership and slow-consumer backlog.
- Source values and keys never enter hint bytes. A directory replacing the hint
  file makes the transaction fail before publishing the proposed new value.
- Profiles of a different selection can produce a hint, but the final comparison
  uses actual delivered versions. Key epoch hints require revalidation without
  creating an input change solely from re-encryption.
- The first generation replacement fixture acquired the launch gate twice. Its
  lock_pending timeout was fixed to use ReplaceHeld under the already owned
  launch/maintenance gates; no production lock policy was changed.
- Separate C01 caller review found and fixed the existing 64 KiB control-record
  truncation and root-admission/integrity metadata overclaim in independent B02
  follow-up commits. Those fixes preserve the accepted storage/authentication policy.

## Actual checks

Deterministic scheduling tests cover startup, missing notifications, exact quiet
and maximum debounce boundaries, duplicates and out-of-order hints. A slow consumer
receives only the latest of 1,000 metadata goals without blocking the producer.

A real watcher process communicates with an actual separately spawned key-session
daemon and real encrypted storage. With every hint deliberately dropped, it
converges after mutation and actual encrypted logical storage replacement, clears
verified current metadata on manual lock, retains the exact idle deadline while
polling, reconnects after actual daemon kill/replacement, preserves durable manual
lock, and inspects current storage when the watcher process is restarted. No
cache-clear action is used. Hint transport loss is injected; storage/IPC/process
boundaries are real. It is not a native keychain/logout or physical power-loss test.

- `C01-real-process-watch-generation-race2.log`: actual process tests pass, 10.482s.
- `C01-hint-schedule-race.log`: focused race pass, including real publication/epoch,
  no-op, malformed hint, atomic failure and narrow replacement path checks.
- `C01-hints-full-race.log`: maintenance/vault/values/backup/profiletransfer/profiles
  full race pass (1.967s/24.782s/72.717s/41.306s/1.314s/1.624s).
- Related vet passes (`C01-hints-vet.log`). Logs are under
  `/tmp/devtools-values-plan`. Native/final-source and consumer checks remain pending.

All areas of this prerequisite unit reviewed; no unresolved eligible finding in
this unit. It does not complete C01 or any native integration gate.
