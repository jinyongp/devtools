# Operation reliability review

Workstream: `d19b52f2-2393-42b3-b04b-22769305bb89`.
Baseline: `ce95f12`, public v0.36.2, protocol 17. Corrected public contract:
protocol 18. Work and validation use main; no additional checkout is required.

## Coverage and review loop

| Area | Reviewed behavior and evidence |
| --- | --- |
| Task edit / legacy adapters / replay / queries | Reserve order membership before forward moves; reject duplicate, foreign or missing included membership. Preserve removed positions, semantic evidence and historical revisions. Existing explicit move repairs `[N,A,N]` without rewriting reads/history. Go and installed preview/replay/history checks cover both future mover and future anchor. |
| Process start/restart and receipts | Distinguish confirmed pre-spawn failure, stopped-before-start, observed spawn and unknown outcome. Done failure replay uses the saved record without adding a receipt field. Preserve execution ID, previous execution, outage and effects. Go tests cover old Done and pending receipts; real restart tests cover post-stop failure and client death. |
| Project lifecycle and diagnostics | Use child completion to separate terminal replay from unfinished work. Mixed batches retain successful/failed siblings and resume only pending children. Status and captured-log remedies use original IDs/location. Public facts use typed allowlists; no private record/control credential is serialized. Unknown effects dominate aggregate partial effects. |
| CLI / Dashboard / automatic value restart | Shared Apply result classification reaches all three. The automatic coordinator still checks observed start, running state and readiness before delivery success; unknown outcomes pause rather than restart blindly. Browser tests cover dashboard changes and delivery status. |
| Singleton / request / maintenance locks | Shared operations barrier plus exclusive request and canonical singleton locks allow independent operations. Store.Lock remains exclusive for cleanup, port prune and instance move/remove. Publication and staging cleanup share a short lock. Causal marker tests and race tests cover exclusion, cancellation and maintenance. |
| Frozen input / vault / backup boundaries | Existing launch/slot/maintenance gates and authenticated inherited leases remain. No new process lock is acquired under a maintenance gate. Existing restart/vault/backup/cleanup checks remain part of the full suite and native CI. |
| Installed / platform / docs contracts | Shared fixture checks real failures, replay, order and Unix TERM contention. Existing Unix, native Windows and WSL adapters carry it; WSL copies the added module. Protocol version assertions, schema IDs/const, PowerShell, browser and skill references agree on 18. |

Pass 1 checked the four reported failures and their callers. Pass 2 found that a
failed record can explicitly carry `launch_outcome_unknown`; classification now
preserves that uncertainty. Pass 3 found stopped-before-spawn success and unknown
effects lost in a batch; both have regression coverage. Same-profile singleton
discovery now has an explicit negative control against unrelated supervisor RPC.

Native Windows found a crash-test assumption: a surviving supervisor confirms
`launch_canceled`, whereas a lost supervisor reports an unknown launch. The test
now checks each recorded cause against its required error rather than requiring
one scheduling outcome. Every retry still preserves identity, no spawn and outage.

Local Playwright initially discovered an old ignored worktree's tests. That run
is not final evidence. `playwright.config.mjs` now fixes discovery to this
checkout's scripts; `--list` reports four tests in one file and the main-only
run passes. Native CI triggers include verification fixtures and this config.

## Verification and rollout

Local validation includes all Go tests, static checks (including vet), affected
installed scenarios (`cli_reliability`, `processes`, `tasks`, `project_lifecycle`),
race tests for tasks/services/lifecycle/cleanup, and four Chromium dashboard
tests. Final exact-SHA GitHub Actions results belong to T07/VFINAL in the
workstream; local results do not substitute for native/WSL validation.

On the same Linux UID1000 host, three v0.36.2 contention samples were
3.093/3.099/3.099 seconds. The initial corrected candidate measured
0.100/0.105/0.102 seconds; all three starts completed before the unrelated stop,
whose duration remained about 3.05 seconds. These are descriptive samples, not a
portable latency budget. CI uses causal ordering rather than a speed threshold.

No storage migration, global CLI replacement, forced service restart or host
configuration change is needed. Old writers take the retained operations barrier
exclusively, preserving compatibility. Downgrading the CLI also restores old
error behavior; it does not repair historical task order. Existing corrupt order
requires an explicit reviewed edit. New attempts after terminal failure require
a new request UUID; old failed requests remain replayable.

Live providers, physical LAN, user trust/hosts changes, receipt compression and
unrelated visual acceptance remain outside this workstream. Publication follows
successful exact-SHA validation through the existing release workflow.
