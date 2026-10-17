# W4D — detached supervisors and local daemons

Task: `f8b78e8c-47b4-4dae-b848-cce2808da630`. Implementation and validation in progress; no completion claim.

## Contract

A detached Windows supervisor receives a new hidden console, remains subject to enclosing host Job policy, and requests no breakaway. The invoking CLI holds no application Job. The supervisor owns the W4P application Job and closes it on exit/crash. A real inherited exclusive lease handle preserves ownership while the launching CLI exits; an inheritable duplicate is allowlisted for the initial supervisor launch and closed in the parent immediately after CreateProcess. Adoption validates the exact private stable lease and clears inheritance before application launch. Unix retains fd 3 and its existing session/group behavior.

Only successful actual application start and durable private record publication expose running state. Windows stop uses the W4P CTRL_BREAK grace and bounded Job termination. Readiness owns independent application Jobs and serializes checks; stop cancels outstanding probes. Control RPC retains exact Host, POST, absent Origin and bearer token checks. Dashboard and HTTP proxy use detached start and native exclusive control listeners. Physical terminal/UAC/browser tests remain NOT RUN under the accepted Actions-only policy.

## Review ledger

| Area | State | Evidence |
| --- | --- | --- |
| Detached console and parent lifetime | pending | Native launcher-exit and supervisor lifecycle fixtures prepared |
| Lease handoff and malformed handles | pending | Actual share-mode lease adoption and closed/pseudo handle checks prepared |
| Actual start/state and terminal metadata | pending | Start failure, early exit 23, grace 130, force 137 fixtures prepared |
| Application Job crash cleanup | pending | Observed process handle assertions on live application and child |
| Readiness/logs/control authentication | pending | Actual native RPC and readiness process cases prepared |
| Dashboard and HTTP proxy | pending | Actual detached lifecycle, singleton/replay, listeners, auth and released sockets |
| Unix callers and compatibility | pending | First affected Unix test suites pass; final checks pending |
| Native x64/ARM64 evidence and limits | pending | Both native Actions runs required; no local Windows execution |

## Review findings under verification

The first ARM64 fixture read a PID file after creation but before its content write. Fixture PID publication now renames a completed file, so existence is a meaningful barrier; live logs also use an observable bounded readiness check. The next run passed all service/tree/readiness and daemon cases on both CPUs. Its real project scenario then exposed a fixture contract error: the partial retry omitted the initial capture=true option, correctly producing process_conflict for the reused peer. The scenario now preserves capture and diagnoses missing result items before dereferencing them. Product singleton option checks remain intact. The corrected scenario and whole native lifecycle suite require a final run.

Unix full Go/vet, affected race and installed processes/project_lifecycle scenarios passed on the W4D implementation. W7I source work is separate and excluded from that evidence.

A later x64 run failed the pre-existing W4P handle-count check with a four-handle increase. That check measured the entire package process after one warmup. Go's Windows runtime itself allocates two event handles per OS thread (GOROOT runtime/os_windows.go), so unrelated fixture/runtime growth was not isolated. Failure accounting now runs in its own fresh test process with GOMAXPROCS=1, warms 20 invalid launches and measures 100 further invalid launches. The allowance stays +2. A separate native Go-overlay negative control deliberately omits failed-launch Job cleanup and must fail with the leak diagnostic. Both the corrected check and this negative control need final native evidence; no product resource ownership code changed.

The isolated aggregate check also observed nine additional handles over 100 attempts, so isolation alone did not establish a valid assertion. It is superseded by the documented Windows Process Snapshotting APIs: snapshots count actual Job/File/Process/Thread resources and record only type counts, never object names. Reference Job and File handles must be visible with the expected types. Those resource types must not grow at all; runtime Event growth is reported separately. Both deliberate Job and pipe-handle leak mutants must be detected. This avoids increasing a global tolerance or assuming the observed delta proves a launcher leak. [PssCaptureSnapshot](https://learn.microsoft.com/en-us/windows/win32/api/processsnapshot/nf-processsnapshot-psscapturesnapshot), [PSS_HANDLE_ENTRY](https://learn.microsoft.com/en-us/windows/win32/api/processsnapshot/ns-processsnapshot-pss_handle_entry).
