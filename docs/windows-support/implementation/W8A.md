# W8A — native product and release gates

W8A integration validation and its last review loop are complete at definition136,
source `50ccb27b5df2d47ad56a1b24570dde66b9e7a2ba`.
[Release39024924795](https://github.com/jinyongp/devtools/actions/runs/39024924795)
passed both full native Windows jobs, macOS/Linux regression and final packaging
with publish=false. Tag/Release/Homebrew/Pages jobs were skipped. This does not
claim actual-PC validation or complete the later W8L/W8R/W9 acceptance tasks.
The accepted policy remains Actions-only with explicit actual-PC limitations.

[Final raw results, runner metadata and archive inspection](../evidence/product/50ccb27b5df2d47ad56a1b24570dde66b9e7a2ba/actions-39024924795/summary.json)
retain all required native Go result files, actual installed/browser/PS results,
Unix job logs, run/artifact metadata and the six archive inspections. Native ZIP
bytes downloaded directly from each native product artifact equal the matching
release-assets ZIP. Each native full suite has923pass/2pre-existing opt-in skips;
the >128MiB maintenance case passes in the separately enabled storage gate.
AMD64 race has268pass, no fail/skip. ARM64 has native contention, no race claim.

The reusable Windows workflow checks the requested immutable source commit against
its checkout. Release packaging requires both native Windows jobs plus the existing
macOS/Linux checks. Each Windows job builds its own candidate ZIP, checks its runtime
identity, installs it through PS5.1, exercises managed project/proxy/TLS and encrypted
profile transfer, then uses the packaged CLI with an actual Chromium dashboard.
The Linux packaging job copies those same checked Windows ZIPs, verifies their
SHA-256 files, and never substitutes cross-built Windows binaries.

Windows amd64 race validation requires cgo, an x86_64 mingw-w64 compiler and a real
libsynchronization.a. ARM64 runs native normal/process-contention checks; it does
not claim a race detector. Browser and Node executable architectures are recorded
separately from the native CLI architecture, including emulation when observed.
Missing Chromium or a required suite failure remains a failure.

## Findings and rechecks

- Concurrent execution-record reads reproduced Win32 sharing violation32 on both
  runners. MoveFileEx temporarily obtains DELETE access while private readers
  exclude share-delete. The SDK retries only that read-open contention, bounded
  to two seconds; ACL, corruption and other failures remain immediate. The
  publisher retains its own independent two-second bound and raw image checks.
  [39021546065](https://github.com/jinyongp/devtools/actions/runs/39021546065),
  source2fad4ff19108f384a37b626b488d01bf4a9d9af4, passes 20 real-project lifecycle
  repeats, three concurrent publication runs and the held-DELETE reader regression
  per native CPU. Earlier green repeats alone were not a root-cause fix.
- A canceled synchronous Windows stdin pipe could leave a reader consuming later
  bytes. PeekNamedPipe now reads only available bytes with context polling, without
  closing caller handles or leaving a background consumer. Partial input/cancel/
  late bytes, chunked Unicode and EOF regressions exercise the native boundary.
- The proxy's former five-millisecond lease probe included private-directory and
  ACL validation, so slow validation could label a free lease as busy. The new
  nonblocking probe validates storage and checks writer intent and the gate once;
  free, busy, canceled and unsafe cases remain distinct. Lost-supervisor and actual
  network callers are required rechecks, not merely a longer fixture timeout.
- Unix-shell children are replaced by equivalent native child fixtures for Windows;
  Unix retains real shell execution. Private fixtures use SDK DACL creation and
  actual untrusted ACEs; Unix exact mode assertions remain. JSON paths are escaped
  before comparing diagnostics; 8.3 pathname spelling is not directory identity.
  The vendored dashboard bundle keeps LF bytes so its pinned digest is unchanged.
- Installed-project retries resume only typed proxy_pending/process_pending with
  the same frozen UUID/input. No I/O or ACL failure is retried by the fixture.
  Raw secret stdin retains its newline, and cleanup stops only the fixture's owned
  authenticated controller. No stored PID or unrelated process is killed.
  [39022416372](https://github.com/jinyongp/devtools/actions/runs/39022416372),
  source949c58b17884dd602718f643bbd2ea0f5dc00c1a, passed the installed candidate
  journey and actual Chromium dashboard on both CPUs. These focused successes did not replace the final full-source gates; those
  gates now pass after the lease-probe correction.

The final validation candidate is `50ccb27b5df2d47ad56a1b24570dde66b9e7a2ba`.
[Release39024924795](https://github.com/jinyongp/devtools/actions/runs/39024924795)
uses publish=false and passed all required jobs and package inspection. Previous4f1b4a0/run39023470729 passed
Linux and both native complete Go-suite steps, but had the diagnosed macOS tool
fixture defect. After correction,0a7380f/run39024170042 passed both complete Unix
jobs and found a Windows cold-TLS test deadline failure below. Canceled or
incomplete jobs are not counted as the final pass.

Further integration corrections:

- Complete native suite39022899915 at source0728a16 had 922 passes, one failure
  and two existing opt-in helper skips per CPU. The sole failure was the port
  fixture's global tool requirement missing its owned native executable on PATH.
  All proxy tests, including lost-supervisor/exposure/routes/TLS and the new
  nonblocking probe, passed on both CPUs. The fixture now supplies that tool on
  PATH while retaining the per-command PATH override contract.
- The first publish=false run found three installed scenarios still expecting
  protocol6; they now assert the already-defined protocol7 contract. The complete
  installed Linux scenario suite subsequently passed, including TTY, transfer,
  provider simulations, task recovery and actual managed processes.
- macOS check:all found common fixtures passing the /var system alias to the
  SDK's no-follow directory guard. Fixtures now resolve their already-existing
  temporary parent before creating private descendants. The SDK's validation is
  unchanged. Isolation checks likewise use a physical existing parent, preserving
  precedence, no-create behavior and native 8.3 spelling independence.
- The native fixture port introduced a fixed /bin/true path into Unix tests;
  macOS has the external tool at another path. Unix fixtures now resolve true on
  the actual host PATH. The affected CLI suite passes on Linux; macOS and both final native candidate gates now pass. This does not change Windows code.
- The standalone browser-fixture Go entry starts a long-lived opt-in development
  server, not an automatic browser test. Actual packaged Chromium is mandatory
  separately. The full storage gate explicitly enables the opt-in >128MiB
  maintenance replacement, so its normal Go-suite skip is not an omitted gate.

- The0a7380f full amd64 suite recorded a one-second client timeout in the
  cold TLS/HTTP2 lifecycle check. That is the observed failure; the exact delayed
  stage was not logged, so it is not described as a diagnosed production TLS bug.
  TLS route validation and the subsequent HTTP handler both inspect private
  storage, whose legitimate read-sharing wait can itself take two seconds. The
  functional test has no one-second performance requirement. Its single-request
  client now has a ten-second bound and records handshake/write/first-byte phases.
  There is no request retry; CA verification, HTTP2, exact forwarded body, stop and
  sibling-listener assertions are unchanged. Three Linux race repetitions pass;
  final native full suites pass on both CPUs. The observed single cold request
  completed in196ms on amd64 (TLS91ms) and59ms on ARM64 (TLS40ms). These are
  observations, not a performance guarantee or proof of the old delayed stage.
- Contributor/release/installed-verification docs still described only four CLI
  archives and unconditional Unix race/main-ancestry behavior. They now describe
  six targets, exact native-tested ZIP reuse, the publish=false branch boundary,
  separate Windows race/installed gates, DEVTOOLS_ISOLATED_ROOT and tag-owned
  Unix/Windows Pages installers. Links and claims were checked against the actual
  scripts/workflows; these product docs are included in the final candidate.

## Review ledger

| Area | State | Evidence |
| --- | --- | --- |
| Publication/security/cancellation | Reviewed | Held-DELETE and concurrent publication regressions, native ACL/no-repair checks and complete final suites pass; source fixtures stay out of production CLI dependencies. |
| Proxy lease and network callers | Reviewed | Free/busy/canceled/unsafe nonblocking probe; lost-supervisor, exposure, routes, TLS/HTTP2/WebSocket and sibling lifecycle pass in both complete suites and network gates. Cold TLS remains one request with full semantic assertions and logged phases. |
| Portable CLI/private fixtures | Reviewed | Native Go923pass per CPU, actual PS/shim contracts and retained Unix race/full installed suites; real DACL negatives and exact Unix modes remain. |
| Installed candidate/browser | Reviewed | Same source/ZIP/version/exe identity; actual PS5 installer journey, encrypted transfer and Chromium login/mutation/reload/CLI/401/403 pass on both CPUs. ARM64 CLI/Node are native; observed Chromium is amd64 emulation. |
| Race/ARM contention | Reviewed | Actual amd64 cgo/mingw-w64 race268pass; ARM64 native ordinary/process/lock contention passes without a race claim. |
| Reusable release dependencies/artifacts | Reviewed | Full jobs required before package; same native ZIP bytes reused; all six CPU/OS archives have valid format/build/version/commit/checksum. Skill matches canonical source and Mac/Linux package comparison passes. publish=false leaves all publication jobs skipped. |
| Support/WSL/actual-PC boundaries | Reviewed | Candidate/unpublished status, Server test-host role, elevated runner metadata, separate standard-user negative checks, independent WSL stores/trust and NOT RUN disclosures agree. W8L binds these exact artifacts; W8R/W9 remain separate. |
| Developer/install/verification docs | Reviewed | Current source documents six targets, per-platform check/race/installed behavior, child-visible isolation and tag-owned Pages assets. Source links and workflow/script claims checked; final candidate includes these doc corrections. |

All W8A areas reviewed after the final results and archive inspection; no eligible
unresolved W8A defect or required W8A check remains. Existing optional Unix
system-network smoke was not requested, not represented as a pass, and does not
complete the separate network workstream.

No local Windows executable is used in this implementation phase. Actual-PC UAC,
CurrentUser trust/browser integration, physical terminal closure, installer/update
observation and final WSL2-to-Windows-browser networking remain NOT RUN. Actions
API/console/browser fixtures do not establish those observations.

Primary toolchain references: [Go race detector](https://go.dev/doc/articles/race_detector),
[Playwright requirements](https://playwright.dev/docs/intro).
