# C02 automatic goal admission guard — prerequisite unit

C02 is active, not complete. This unit adds the tracked per-command
`values_update = "notify" | "restart"` opt-in and the internal service request
contract which the forthcoming detached automatic coordinator will use. Omitted
policy is notification only; no automation is dispatched by this unit.

An automatic request fixes the original execution and opaque input goal. Inside
the existing process operation gate, its original must still be confirmed running
and its current canonical project must still opt in. After actual materialization,
the prepared delivery identity must match the goal before launcher preparation,
admission or stopping the original. A changed goal, revoked policy or manual stop
therefore preserves the original/stopped state. This requires the frozen launcher;
there is no legacy stop-first fallback.

A previously created successor is reconciled by its exact request/execution ID,
previous execution ID and recorded goal. It cannot be replaced merely because its
original has already stopped. Existing receipts fix the entire request fingerprint.
No value bytes, launch ticket or value-derived plaintext fingerprint are added.

## Review coverage

Reviewed config decoding/validation, process gate, request receipt/replay, current
project identity/opt-in, materialization/release, early existing-successor return,
unchanged manual start/stop/restart and frozen launcher callers. The early review
found two gaps before commit: rejecting a non-frozen fallback was necessary, and
replaying a pending request after original stop must reconcile its existing
successor before applying the original-running check. Both were corrected.

The actual-process regression first changes storage behind a captured goal,
verifies old execution remains running, revokes opt-in, then performs a valid
replacement. It injects loss of the completion receipt while retaining the real
successor and verifies exact-UUID reconciliation. A late automatic request against
the already stopped original cannot resurrect it. Config tests cover omitted,
notify/restart and invalid policy spellings. Affected frozen restart/client-crash
and stop-response-loss regressions are included in the race run.

Automatic dispatch, bounded burst/failure handling, readiness, status and product
integration are still pending in C02. This unit is not its completion evidence.

Final unit validation: project/services/CLI focused race pass
(1.021s/2.105s/12.850s), related vet pass. Logs:
`C02-goal-guard-final-race.log`, `C02-goal-guard-vet.log` under
`/tmp/devtools-values-plan`. All prerequisite review areas reviewed; no remaining
eligible unit finding. Scheduler and native integration are separate remaining work.
