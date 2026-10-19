# C01 managed watcher consumer review

The existing managed supervisor owns one metadata watcher after confirmed child
start. It stops with that supervisor. No new persistent daemon, whole-value cache,
implicit unlock, application restart, or repeated foreground command is introduced.

`record.value_watch` is the last background observation: state, safe reason,
observation timestamp, optional opaque goal identity and manifest verification
flag. It is live through authenticated supervisor RPC; the terminal record can
retain the last observation with its timestamp. It does not replace the recorded
original `input_version`, claim application reads, or authorize a launch. Unknown
legacy deliveries are not assigned a manufactured delivery version. Dashboard
process details show this background state; existing query-time observations
remain available separately.

## Coverage ledger

- Reviewed: Unix and Windows start callbacks, publication under the record mutex,
  authenticated status serialization, cancellation, terminal publication, passive
  metadata inspection, CLI schemas and dashboard rendering.
- Reviewed: hint transaction/schedule/storage replacement and daemon reconnect
  coverage from C01-hints-engine-review.md remains applicable.
- Corrected before commit: Windows child-start callback held the record mutex
  while starting a watcher which synchronously reads the record. Release the mutex
  after publishing child/probe state and before watcher creation. No callback
  reads a partially published record; failed storage publication unlocks too.
- No automatic restart policy is implemented here. C02 and native/final integrated
  gates remain separate; this is not a native Keychain/logout or power-loss proof.

## Validation

`TestManagedSupervisorValueWatchReconcilesWithoutHintsAndKeepsAppAlive` uses an
actual separately spawned supervisor, child, key-session daemon and encrypted
storage. It checks initial current state, changed goal after removing commit
hints, passive polling preserving idle deadline, manual-lock verification pending
without goal/integrity claims, explicit-unlock reconvergence, unchanged original
child environment, exactly one execution, and no value/authority in watch status.
The focused race run passed (8.078s). Broader affected checks are recorded with the
commit and workstream validation basis after completion.

Final unit checks: services/cli/dashboard/lifecycle full race pass
(7.342s/54.908s/3.461s/1.814s); related vet pass; Windows amd64 services and
ARM64 CLI cross-compilation pass (compilation only). Fresh candidate Chromium
browser run passes all three scenarios in 19.6s, including background pending and
locked verification labels; 33 dashboard Node checks pass. Logs are
`C01-consumer-{full-race,vet,browser}.log`, `C01-dashboard-node.log`, and
`C01-windows{,-arm64}-compile.log` under `/tmp/devtools-values-plan`.
All consumer review areas are reviewed; no unresolved eligible unit finding.
