# C02 final implementation acceptance

The explicit managed-server restart policy is implemented and reviewed. Default
notify, foreground and ended one-shot behavior are preserved. Scope covers the
whole goal guard, detached coordinator, operation/sidecar protocol, storm and
failure latches, readiness, current-project opt-in, passive lock, schemas and UI.

| Area | Status | Evidence |
|---|---|---|
| Actual frozen goal/old execution before stop, same-UUID replay | reviewed | C02-goal-guard-review.md; real goal/replay test |
| Helper ownership, crash/late response, one attempt, first completion | reviewed | C02-auto-coordinator-review.md; actual coordinator kill |
| Pre/post-stop failure, readiness/outage, manual reset, rapid goals | reviewed | real automatic/outage cases, Windows x64/ARM64 native pass |
| Config default/notify, opted server, one-shot/siblings | reviewed | project/config and real coordinator tests |
| UI, discovery and secret-free status | reviewed | full four-case local browser, 33 Node checks |
| Installed child + verified TLS proxy, lock/unlock convergence | reviewed | C02-installed-proxy-review.md, actual installed local journey |
| OS private sidecars and detached process | reviewed | C02-windows-crash-fixture-review.md; fresh native x64/ARM64 pass |

Review corrections: removed non-frozen auto fallback; reconciled same UUID before
original-running check; retained parent completion beside current successor;
serialized first terminal completion; required actual running delivery before
reporting applied; fixed Windows fixture DACL through production secure writer.
All affected callers and failure paths were rechecked after these changes.
Historical intermediate pending statements remain in unit reports as chronology;
this final ledger supersedes them for C02 implementation acceptance.

Exact 1a6d094 Actions run 39280514924 completed successfully: 26 required jobs,
three publication jobs skipped. Both Windows native full suites contain passing
actual automatic goal, coordinator death, outage/readiness, one-shot/storm and
watcher tests, with no failure events and CLI package pass. Linux/macOS full
suites and native provider/WSL gates passed in that same run. See the structured
proof for job IDs, test timings and hashes. Fixture console alignment alone did
not fix the earlier failure; the secure sidecar writer did, and native revalidation
now confirms it. No production permission was weakened.

The separate installed automatic TLS-proxy journey added in ad330f0 passed on
local Linux/WSL. That test-only addition does not alter the 1a6d094 coordinator
runtime; its execution is not mislabeled as an older native artifact. The later
backup KDF bound likewise does not change the automatic-launch implementation.
C03 still owns final-source integration/convergence measurements and Z01 the
whole-workstream review. No remaining eligible C02 implementation defect or
pending unit area. No arbitrary sandbox/same-user containment or app-credential
validity guarantee is claimed; already running apps keep their delivered env.
