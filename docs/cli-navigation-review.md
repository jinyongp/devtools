# Navigation implementation and review ledger

Scope: [accepted plan](cli-navigation-recovery-plan.md), workstream
`73d68d31-7851-4df0-9461-53364c3cb7a3`. Implementation remains on main.
Baseline `46dfba1417e00b793fc2a5d2c3c4864d8ba1b28f`; no release or user-data cleanup.
Evidence below is local source/fixture evidence. Final commit-bound platform/CI
results are recorded in the workstream validations, including V_INTEGRATION;
completion requires those results to pass.

| Area | Review state | Evidence / remaining work |
| --- | --- | --- |
| T01 registry inventory | reviewed | 198 baseline + cleanup maintain = 199 canonical entries classified; registry coverage test; nested/diagnostic exclusions recorded |
| T02 action preparation | reviewed | parser/static/schema resolver, typed stop/new UUID, saved-map/private/manual/native UI cases and public original-input bindings pass; installed recovery argv executed unchanged |
| T03 preview/apply | reviewed | emitted ready argv applied unchanged; exact child/root; changed file blocked before publication; stdin/private acquisition cases pass |
| T04 selection/cache/columns | reviewed | filter→sort→page; 83 command rows and 10000 indexed snapshot rows; root/condition binding; text-only/unknown/duplicate columns checks |
| T05 process navigation | reviewed | 313-record fixture, service/current vs history, all pages, profile/location scope, failed restart preparation and replay regressions |
| T06 exposure/delivery navigation | reviewed | resolved/expired/canceled history separated; unknown/auth/recovery retained; protected cursor denied after vault lock |
| T07 remaining collections | reviewed | native task filters and stale completion preserved; task suite passes; port/instance intersections and all-profile selection; values-use observations reuse current metadata |
| T08 retention and maintenance | reviewed | TTL/reference/replay and focused race fixtures pass; corrected shared body budget, bounded cache prune, old preparation history and authenticated delivery resolution evidence; final native in T12 |
| T09 explicit storage preparation | reviewed | explicit process/archive/delivery/log adapters, crash/resume and old active writer checks; missing body guard handled by doctor; native write denial remains T12 |
| T10 consumers and documentation | reviewed | protocol 23 schema/help/completion/examples/guides; CLI/dashboard/value/vault suites; 40 dashboard JS tests including lazy detail and actual execution IDs |
| T11 scale and concurrency | reviewed | 313/1000/10000 metadata fixtures; live RPC 1/2 and unrelated0; current1.6/2.2KB; 10000-row independent cursor and focused maintenance/cache/publication race pass |
| T12 integrated review | source reviewed; platform validation tracked separately | all areas rechecked; aggregate static/vet/race pass and installed Linux write-denial parity pass; final main SHA and native Actions results belong to the workstream validation records |

Confirmed regressions corrected during implementation:

- Attempt metadata initially resolved current config before an existing pending
  execution replay. It now derives the original record first. Pending/completed
  launch and stop replay regression fixtures pass.
- The action resolver originally checked parser/static predicates only. It now
  also consumes the registry's declared input alternatives, so conflicting raw
  value/stdin or JSON/child sources cannot be emitted as ready.
- Native task filtering originally changed typed event results into decoded maps.
  Filtering now selects original public values without persisting internal indexes.
- Native task cursors now bind their data root. Same-root continuations remain
  independent of active journal availability; cross-root cursors are rejected.
  Obsolete non-indexed page snapshots are rejected; cleanup can still recognize
  their expiry metadata. No user-authored task TTL was introduced.
- Native vault recovery incorrectly treated OS authentication as a missing argv
  input, hiding a complete unlock command. It now publishes ready
  `vault unlock --interactive`; passphrase inputs remain private acquisition.
  A synthetic protector reproduces default no-UI failure and explicit UI success.
  The user's original macOS error log was deleted, so that exact failure cause
  remains unknown; native platform verification is separate.
- CAS reclamation originally exceeded the maintenance artifact budget and walked
  unrelated control JSON. It now shares the budget and resumes a body phase;
  only real body owners are collected. Receipt-only shared bodies are protected.
- Bounded delivery maintenance now preserves authenticated evidence of newer
  accepted work. Retiring a successor cannot resurrect a resolved old failure.
- Static vet detected copying a sync.Map inside a scoped App copy. The compiled
  action contract cache is shared by pointer across those copies.

Current local checks include focused protocol/ports/CLI/dashboard compilation,
preview round trips, collection/snapshot/query suites, service replay fixtures,
exposure and delivery current/history fixtures, and the complete task suite.
The aggregate `./scripts/project check -p=1` passed with GOMAXPROCS=2, including
static checks, vet and all Go race tests. An earlier unrestricted parallel run
timed out across several packages; serial package execution passed. The remaining
real failures were old fixture formats and protocol-22 output assumptions, now
updated to prepare storage explicitly and select the intended scope/history.
Installed guidance/presentation/reliability/cleanup/value-update/lifecycle and Linux
native write-denial scenarios passed. The latter includes the reported project-only
doctor command, process current/history, protected metadata, unchanged data/idle,
and old-writer interoperability. Cross-platform CI and the full installed suite
are final workstream gates; intermediate results do not stand in for those gates.
