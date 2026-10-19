# C03 automatic application final integration review

A12 first-stage and B03 bundle/usage acceptance are complete. Current production
code is unchanged from exact 1e4bebd native run 39286793430 (26 required jobs
successful, three publication jobs skipped). C03 adds only a reviewed measurement
tool, actual measurements and operating/Skill guidance; production/source-native
reuse is checked explicitly rather than labeling later scripts as native runs.

| Condition | Reviewed integration evidence |
| --- | --- |
| C01 unrelated / hidden common / same value | Actual coordinator opt-in/override fixture in both Windows full native suites and Unix full race, plus installed frontend unselected/current versus selected/notify pending |
| C02 epoch-only reencryption | Actual profile rotation/snapshot tests preserve logical input identity; SameInput ignores epoch while admission still rejects stale key epochs. Existing current-state watcher does not dispatch restart |
| C03 continuous updates/failure/storm | Actual pre-stop latch, post-stop missing executable/readiness death, coordinator kill, late completion and new goals; 54 new convergence samples and actual three-success/fourth/fifth pause |
| C04 bundle/override effective selection | B01/B02 full ledgers, exact frozen goal guard, same UUID response-loss reconciliation; current installed selections and all twelve native nonempty-bundle transfer directions |
| C05 loss/reconnect/restore/lock | Actual separate watcher/key daemon processes with hints dropped, worker/watcher death, generation replacement and unlock; passive locked verification_pending, no idle touch; periodic/latest-only reconciliation |
| A01 managed explicit opt-in only | Default notify and siblings/ended one-shot preserved by real tests; installed notify original stays alive; opted installed TLS proxy child receives revision and readiness through same route |
| L03–05/E03–06 fault matrix | Native actual frozen launch/lock before/after admission, old stop mutation, post-stop outage, same UUID, coordinator/daemon death/unknown and actual protected restore/legacy barrier cases in A12 proof |
| D/U/S/V/F across A/B/C | A12 coverage ledger and raw matched baseline/runtime samples, B03 installed/schema/UI/Skill evidence, new C03 actual convergence/storm raw rows, precise operation/recovery guidance |

The WSL evidence distinguishes local actual WSL process/race/PTY coordination
from the disposable owned distro's real manual/unlocked termination/restart.
No claim is made that a managed application survives distro termination or that
power loss/kernel PID recycling was forced. Real macOS Keychain signatures and
Windows private ACL/process/DPAPI tests retain their documented limits. Source
only/cross-compile/fake results are not substituted for native acceptance.

Reviewed the full C01/C02 source and consumers with accepted A/B boundaries:
no raw value cache, no implicit unlock from polling, old execution/opaque goal
fixed before stop, actual admitted frozen payload, no restart shell in key daemon,
one persisted automatic operation, first terminal response wins, successor-owned
parent completion, bounded successful burst and durable failure/unknown pause.
Manual retry starts a new authorized operation after inspecting current state;
values are never automatically rolled back and siblings are never stopped as
failure recovery. Readiness remains separate from external credential validity.

Review corrections: the new measurement tool now preserves partially completed
rows on failures, finalizes output even if binary identity fails, confirms cleanup,
and checks actual latest watcher goal rather than inferring it from time. The
Skill and public process guide now explain default notify, explicit managed opt-in,
foreground/one-shot exclusions, outage/unknown, replacement IDs, cap and explicit
recovery. All changed/unchanged callers and documented examples were rechecked;
no remaining eligible C03 integration finding. See C03-convergence-harness-review.md
and C03-convergence-results.md for exact methods and limits.

Current full check:all passes (full Go race/vet, official Skill/package and
all script/static checks). Actual installed vault/profiles/value_updates/
output_redaction/vault_session also pass, including real TLS automatic delivery,
lock/unlock convergence, pipe/PTY output and two separate PTYs. Structured
acceptance evidence and hashes are in evidence/C03-integration.json. Current four-case browser/native product results are
reused only because no Go/UI/JS production source changed. Z01 remains the final
whole-workstream review gate; no release or actual user vault migration occurs.
