# C02 automatic coordinator integration — final unit review

Extends the reviewed goal guard with explicit managed-only opt-in dispatch from
the existing watcher. Single-operation detached helpers survive the old
supervisor's shutdown. Vault/key-session code never executes project commands.
No additional persistent orchestrator or reload shell is introduced.

One safe sidecar per original execution fixes goal/old ID/request UUID before
helper spawn. There is no automatic resubmission, even for a later goal after
failure. Existing process receipts and actual frozen-input admission govern the
launch. A pending operation without completion for 90 seconds reports unknown
outcome and remains paused. Manual restart is explicit retry and clears the chain.
Readiness is a separate existing Wait check (30 seconds); a failure never rolls
back values or kills successful siblings. Existing proxy preparation is reused
noninteractively; inability to satisfy preparation fails before old stop.

Burst history follows automatic successors only: at most three replacements in
30 seconds. It resets after a quiet window or explicit manual restart. Parent
completion gates later automation, including readiness failure. The successor
retains that upstream completion in its own private directory, so pruning an old
execution cannot disable updates of a long-lived current server. Sidecars contain
safe version identities only, with bounded private reads and strict decoding.
Completion transitions use a per-operation file lock to preserve the first final
outcome across concurrent/late replies.

## Review coverage / corrections

- Reviewed goal guard/operation receipts/materialization before old stop in the
  prior prerequisite unit; recheck extends to automatic callers and ancestry.
- Corrected old-parent retention coupling: upstream completion is now retained
  beside the live successor; no indefinite dependency on old execution history.
- Corrected final status transition race: serialize completion using the existing
  private file-lock facility rather than check-then-write without a lock.
- Reviewed command policy decoding/default/discovery, detached Unix session and
  Windows console creation, clean parent environment, bounded helper input,
  original/replacement identity, canonical project/opt-in validation, failure and
  storm latch, passive locked observations, status schemas and text-only UI.
- Re-reviewed: actual post-stop spawn/readiness failure, confirmed running
  delivery, explicit unknown outcomes, status ancestry/retention, first-terminal
  completion lock, existing manual lifecycle and fresh browser/UI callers.
  Native final-source product acceptance remains the separate C03 gate.

## Actual checks so far

Real automatic coordinator, supervisor, child, encrypted storage and key-session
processes: opt-in replacement receives new selected secret; hidden common and
same-value changes do not replace; ended one-shot is not repeated; pre-stop tool
failure retains old app and latches later goals; manual restart resets the chain;
three rapid successful replacements then pause. Removing the old completion
sidecar simulates retention and the successor's own completion still works.

An actual owned coordinator is killed during the old application's stop barrier.
The original completes stop; the canceled frozen launcher cannot spawn afterward;
identical request retries keep exactly two records. Only the safe status timestamp
is advanced to test the 90-second abandonment deadline without sleeping. The
process and IPC failure itself is real. No kernel PID reuse or power-loss claim.

`C02-auto-storm-race.log` passes (services 1.030s, CLI 22.319s);
`C02-coordinator-death-race.log` passes (CLI 2.712s). Private logs live under
`/tmp/devtools-values-plan`. Final coverage and validation remain pending.

Confirmed fault/recheck: `C02-auto-outage-race.log` failed the actual post-stop
spawn-removal case because the coordinator reported applied. Process Apply can
return a successful mutation/record with a child that already failed; that is
not delivery. The coordinator now requires StartedAt and a running result before
claiming delivery or beginning readiness. Actual missing-executable-after-stop
and application-exit-during-readiness cases are rerun against the fix. Original
process mutation semantics remain unchanged; the caller now distinguishes them.

Final consumer validation: `C02-auto-complete-race.log` passes all affected
project/services/CLI/dashboard/lifecycle packages (CLI 93.284s, services 6.703s).
Related vet and 33 dashboard Node checks pass. Fresh compiled CLI browser run
passes all four cases (30.2s); final automatic UI recheck passes (11.6s), including
applied replacement ID, preparation failure pause and secret-free display.
`C02-auto-final-fault-race.log` passes actual death/post-stop failures (13.671s).
`C02-auto-sibling-final-race2.log` passes actual successful readiness, default
notify sibling preservation, one-shot, failure/manual retry and burst cap (24.343s).
A first success-probe fixture used a 1-second timeout; the race runtime itself
waits about one second at child exit, so that fixture timed out. A 5-second probe
bound fixes the fixture; no production timeout was weakened. Windows amd64 CLI
cross-compiles; this is compilation, not a native-run substitute.

All C02 implementation areas reviewed; no unresolved eligible implementation
finding remains. Native installed/proxy/convergence/performance integration and
Z01 holistic review remain separate mandatory gates. No release was published.
