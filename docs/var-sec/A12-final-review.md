# A12 first-stage implementation acceptance

Scope is the accepted A-stage crypto/storage, OS protection, reusable key session,
IPC/locking, frozen launch, output filtering, migration/recovery and CLI/dashboard
integration. [The coverage ledger](A12-acceptance-ledger.md) maps every K/I/L/E/O/B/P
and U/D condition to its implementation fixture; [performance results](A12-final-performance-results.md)
retain all raw timings and resource observations. C/B implementation now present
was exercised too, but their own stage acceptance remains B03/C03. Z01 is separate.

## Coverage and review loop

| Area | Final scope review |
| --- | --- |
| Encrypted slots/full sealed manifest/catalog, public-only/sticky sealed | reviewed; tampering/missing/nonce/wrong-key/batch/source-kind boundaries and all readers/mutators |
| Native protectors, private storage/IPC, cached session and manual lock | reviewed; bilateral actual UID/SID/local pipe, native denial/cancellation, idle-no-touch and durable manual latch |
| Snapshot/admission/generation/epoch, actual process death and receipts | reviewed; old retained before admission, drain after admission, exact UUID, safe unknown, no numeric-PID recovery |
| Streaming output and sensitive lifetime | reviewed; independent randomized reference/all partitions, real stdout/stderr/PTY/capture, cancellation/backpressure/bounds and old-app patterns |
| Migration/restore/legacy/rewrap/transfer | reviewed; actual durable crashes, actual old 89100eb CLI/dashboard/MAC receipt, secret scan and new target-local authority |
| CLI/UI/schema/diagnostics/completion/installed products | reviewed; actual shared paths, passive key inspection, hidden input, no get/argv secret, secret-free results |
| Native OS product gates | reviewed/pass; exact 1e4bebd run 39286793430, 26 required jobs successful, publication skipped |
| Performance and resource accounting | reviewed; matched fixture, raw samples, empirical tails, cold/warm/API/RSS/alloc/redactor distinctions and explicit limits |

Reused per-unit source reviews A00–A11 and N01–N05 only where the relevant runtime
and execution conditions still hold. A12 additionally reviewed native-required
selection/skip protection, actual installed cleanup, original/baseline identity,
input cost limits, full fault mapping, and final measurement applicability.
Confirmed corrections in this stage: native crash fixture private Windows ACL;
separate recovery-client operation deadlines on loaded macOS; real provider
unwrap accounting; native nonempty bundle transfer coverage; and the production
archive KDF resource bound. The protection guide also now distinguishes unsigned
public-only profile bodies from root-catalog authentication and full sealed
manifest authentication, matching the existing native-passing regression.
Each correction has a separate implementation/review commit.
After fixes, affected callers/contracts were rechecked. No security check, OS ACL,
KDF cost for own exports, output fail-closed behavior or admission barrier was
weakened for performance or CI. The broader Keychain ACL proposal was withdrawn;
creator-app protection remains and signature changes can require explicit OS approval.

## Actual checks and provenance

Local current production source 1e4bebd: `bin/devtools run check:all` exit 0
(full project, Go vet/race, dashboard/static/installer/release/Skill/script gates),
`bin/devtools run test:installed` all 30 scenarios exit 0, and explicit current
CLI Playwright all four cases pass (30.8s). The installed automatic scenario uses
actual launcher/key session/readiness/verified TLS proxy; it does not modify
system network trust or a real user vault. Local WSL two-PTY/manual-crash checks
are distinct from the actual separate owned distro termination/restart gate.

The final [native evidence](evidence/A12-native-1e4bebd.json) validates exact
source 1e4bebd in [Actions run 39286793430](https://github.com/jinyongp/devtools/actions/runs/39286793430):
26 required jobs succeeded and all three publication jobs were skipped.
All twelve native bundle exchange directions passed, preserving bundle IDs and
members and selected env slots while changing target-local authority.
All required artifact assertions pass. Windows x64 and ARM64 full suites already pass 36
specifically required fault/security tests plus 39 package pass events each;
actual different-SID/UNC checks are separate required preflights. Actual Mac
Intel/ARM Keychain and Linux Secret Service call counts have one cold unwrap,
zero warm unwraps for 10/100/1000 Use batches plus 32 concurrent uses, one after
clock-driven expiry, zero manually locked, one explicit unlock (three total).
Those are requests through the actual native helper, not syscall counts or
1,000-key delivery counts. Separate snapshot tests cover one key access for
10/100/1,000 slots. Actual WSL phases report the exact candidate binary identity.

Initial attempt's emulated x64 pwsh.exe installer terminated with 0xC0000005 at
explicit PATH registration (exit -1073741819, line 181); this is a real failed
check, not ignored success. With the workflow finished, the same exact job and
its dependent gates were rerun without changes. Installer rerun passed (2m56s).
Native suites/provider/product/WSL artifacts remain the successful attempt-1
results at the same SHA, not fictitious rerun measurements. No .NET crash-dump
root cause is claimed. The final proof preserves both attempts/provenance.

Measured runtime f3722b2 differs from accepted production 1e4bebd only in backup
archive cost handling; warm snapshot/session/launch/redactor paths are unchanged
(`git diff --name-only f3722b2..1e4bebd -- internal cmd ':!**/*_test.go'`). Thus
matched 89100eb/candidate latency/RSS/component observations remain applicable.
No throughput/latency SLA, physical erasure, actual eight-hour wall wait or
same-user malicious-agent containment is inferred. Tail variability remains in
raw samples; performance was not improved by bypassing protection.

There is no remaining pending A12 review area or eligible defect; all required
native/installed evidence is now satisfied. The later installed metadata
assertions in 5be2322 also pass locally (A12-installed-usage-review.md); they do
not change production runtime or get mislabeled as earlier native executions.
B03/C03 and Z01 remain independent mandatory gates. Release publication, actual
user migration, physical-LAN testing, production Developer-ID/notarization and
already-delivered app environment revocation remain outside this workstream's
accepted guarantees. Historical pending notes are chronology, superseded only
by this final scoped ledger and its actual evidence.
