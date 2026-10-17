# W4D — detached supervisors and local daemons

Task: `f8b78e8c-47b4-4dae-b848-cce2808da630`. Reviewed code: `088a1da1ff058214cc5ad8f21736581e8d064a6a`. Native result and final scoped review: PASS. Shared definition revision: 183.

## Behavior and ownership

Windows supervisors receive a new hidden console and remain subject to enclosing host Job policy. No breakaway is requested. The invoking CLI owns no application Job. The supervisor owns W4P application Jobs, so CLI exit preserves the supervisor while supervisor crash closes its Job and reaps the application tree.

An allowlisted inheritable duplicate of the exclusive stable lease survives initial CLI exit. Parent handles close after successful creation; adoption validates the exact private file and exclusive data access and clears inheritance before starting applications. Closed/pseudo/non-file handles are rejected. Unix keeps fd 3, its existing session/group semantics and its existing shell/update behavior.

The Windows runner persists actual environment selection and successful child start before publishing running state in memory/RPC. Failed starts expose no false running record. Stop uses authenticated RPC, CTRL_BREAK grace and bounded whole-Job termination. Readiness serializes independently owned probes; timeout reaps a probe tree without stopping the application. Capture uses the existing bounded private log store. Control RPC keeps exact Host, POST, absent Origin and bearer token requirements. Dashboard and HTTP proxy use the detached launcher and exclusive native control sockets.

## Actual validation

[Actions 39011474499](https://github.com/jinyongp/devtools/actions/runs/39011474499) passes on both native CPUs at the exact reviewed commit. Each has 21 service/lease test rows, 26 complete lifecycle rows, two real detached daemon rows and 28 process rows. Required JSONL results have zero failures/skips. Prior storage/path/port/console gates and native package checks pass too. Normalized metadata and raw results live in W4D-actions-39011474499; no executable archives are committed.

Real child fixtures verify launcher exit, supervisor crash, graceful 130/forced 137, early root exit 23, inherited lease release, start failure, actual selected environment, readiness, captured logs and rejected control requests. The actual exported CLI supervisor/preparation path drives project multi-start, same-UUID replay, selected restart/down, unchanged peer identity and partial readiness retry using the same child ID. Public CLI daemon gates remain for W7C. Physical terminal-close/UI observation is NOT RUN.

Unix full Go/vet, affected race and installed processes/project_lifecycle scenarios pass. W4D-unix/basis.json records the actual source basis: Unix-compiled Go sources did not change between the initial W4D implementation and the reviewed native commit. Subsequent W7I work is excluded from this task's code/evidence.

## Final review loop

| Area | State | Findings, fixes and rechecks |
| --- | --- | --- |
| Detached console and CLI lifetime | reviewed | New hidden console; no temporary CLI Job or breakaway. Actual launching-process exit preserves live supervisor/application, then authenticated stop releases lease |
| Lease handoff and resources | reviewed | Duplicate only the actual disk lease, reject closed/pseudo handles, allowlist inheritance, close parent duplicate on failure/success, validate adopted identity/exclusion. Existing inherited lease checks pass |
| Start/state and terminal metadata | reviewed | Copy/persist next record before publishing running; missing executable has no StartedAt. Actual env/inject selection and exit/stopped/interrupted metadata pass |
| Application/probe Job lifecycle | reviewed | Real application and descendant handles become signalled after stop/crash. Hanging readiness tree is reaped independently; live application survives probe timeout |
| RPC, readiness and capture | reviewed | Unauthorized token/Origin/Host/method cannot stop live service. Real readiness and private capture pass; stop cancellation remains bounded |
| Project lifecycle and callers | reviewed | Real CLI preparation/supervisor path and all 26 lifecycle rows pass on both CPUs. Fixed a fixture that changed capture=true to the default during reuse; existing process_conflict behavior preserved. Partial retry keeps peer and delayed child IDs |
| Dashboard and HTTP proxy | reviewed | Actual detached start/status/singleton or replay/stop, unknown-route HTTP, auth rejection and exclusive socket release pass on both CPUs |
| Meaningful failure checks | reviewed | Fixed PID-file creation/content race by publishing completed fixture files. Replaced aggregate handle counting with actual owned-resource accounting and mandatory leaking Job/File negative controls, described below |
| Unix and release boundaries | reviewed | Full/vet/race and installed scenarios pass; no Unix lifetime implementation change. Actual runner metadata is retained. W7I, W5H/W5E and final product/W9 gates remain separate |

All W4D areas reviewed; no eligible unresolved finding or required unexecuted W4D check remains.

## Failed-launch resource accounting

An aggregate handle-count check intermittently grew by four, then nine handles even in an isolated warmed process. Native snapshots on both CPUs showed Job 1→1 and File 9→9 while thread/event/timer/IO resources grew. A failed CreateProcess owns Job/File resources and creates no child Process/Thread handles. The final check isolates the test process, disables GC finalizers, warms 20 failed launches and requires **no growth** in either owned resource type over 100 additional failures. Runtime resource counts are recorded separately; no global tolerance is increased.

Documented PSS captures handle values. Dynamically loaded NtQueryObject(ObjectTypeInformation) obtains each type because PSS does not populate type names for every handle. Object names are never queried or recorded. Reference Job/File handles must be visible; query errors fail except handles already closed concurrently. This instrumentation exists only in tests. Separate native Go overlays intentionally omit failed-launch Job close and pipe close; both must fail with the matching resource-type diagnostic. Both CPUs detect both deliberate leaks. Expected failing mutant logs are preserved as negative controls, not product failures.

Sources: [PssCaptureSnapshot](https://learn.microsoft.com/en-us/windows/win32/api/processsnapshot/nf-processsnapshot-psscapturesnapshot), [PSS_HANDLE_ENTRY](https://learn.microsoft.com/en-us/windows/win32/api/processsnapshot/ns-processsnapshot-pss_handle_entry), [NtQueryObject](https://learn.microsoft.com/en-us/windows/win32/api/winternl/nf-winternl-ntqueryobject). Go runtime/os_windows.go documents per-thread runtime handles. No additional production dependency or local Windows execution was introduced.
