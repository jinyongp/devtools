# N05 actual restart-client death review

The fault-matrix audit found that coordinator/launcher process death and lost HTTP
responses had tests, but killing the actual restart client at a frozen preparation
or old-stop boundary lacked a direct integrated test. Added a private test-binary
entry, using production PrepareFrozenInput/DependenciesFor and real encrypted
storage, key-session IPC, supervisor, launcher, application and OS process kill.
No product test hook, exposed control command or caller-provided PID termination
was introduced.

## Found and fixed

When the client dies during an acknowledged old stop, its launch pipe closes and
the uncommitted launcher exits without spawning. The original app can finish its
stop afterward. The same request UUID correctly reused its original execution,
but lease-based recovery returned `interrupted` with `outage=false` despite the
now-confirmed stopped old app. Direct reproduction failed in
`/tmp/devtools-values-plan/N05-real-client-crash2.log`.

Use the existing finishInputPhase contract when reconciling a lost supervisor,
returning an existing request execution and reporting current status. It derives
outage from the frozen committed phase or confirmed previous EndedAt. A still
active old app does not become an outage. Passive status never rewrites the
historical record. No input re-materialization, old PID guess or new UUID is used.

## Review coverage

- Before admission: kill the real client after materialization, confirm old app
  remains running, retry the identical UUID and start exactly the original target.
- During old stop: use an app-produced barrier, kill the real client, release the
  old app, observe loss of the candidate's real supervisor lease, and retry. The
  candidate never started, the result reports outage, and three further replays
  keep the same ID with exactly two retained execution records.
- Tests distinguish passive `interrupted` from a persisted EndedAt: the first
  fixture incorrectly waited for a query to write a terminal record. Its timeout
  is not a product finding; the mutation retry acquires the lease and persists it.
- Independent lease recovery cases cover admitted/old-active, admitted/old-ended
  and committed phase. A late old stop updates passive status and existing-target
  retry without writing storage on the query.
- Reviewed status/start/recovery callers, terminal receipts, before-admission old
  app preservation, existing stop-response-loss behavior and lifecycle consumers.
  Existing immutable completed receipts remain historical operation results;
  process status reports current observations. No unknown outcome becomes success.
- Owned cleanup releases the app barrier, stops only fixture executions and retires
  its private vault worker. Client args contain metadata IDs/paths only; values
  reach production snapshot paths and the fixture's explicit app observation file.

Focused actual-client/recovery/stop-response-loss race checks passed (CLI 5.391s,
services 2.175s; `N05-real-client-crash-fixed-race.log`). Whole services/CLI/lifecycle
race passed (7.315s, 47.772s, valid lifecycle cache) in
`N05-client-recovery-full-race.log`; related vet passed. Windows amd64 CLI
test compilation passed; it is not a native execution claim. N05 remains active
until its full current-source native/fault/transfer coverage is recorded.
