# W4P — Windows owned process launcher

Task: `ada0f70c-2e8c-478b-9da6-42ad135131e0`. Reviewed code: `30576fb05d9cdb7290630b70bddd4c06e06e0b5d`. Result: native checks and last scoped review PASS.

## Boundary

CreateProcess receives PROC_THREAD_ATTRIBUTE_JOB_LIST and a non-inherited kill-on-close Job at creation. There is no start-then-assign interval. Only duplicated file stdio handles enter the application HANDLE_LIST; the separate supervisor lease handoff belongs to W4D; invalid/pseudo/non-file stdio is rejected. Job breakaway is disabled. The parent owns process/thread/job/pipe handles and cleans each failure path, including a rejected or panicking start-publication callback.

Normal direct-child exit retains its exit code and closes the Job before draining inherited pipes, cleaning descendants that outlive the root. Cancellation sends CTRL_BREAK only to the created child group, waits up to three seconds for the owned Job to become empty, then terminates the Job. Wrapper cancellation is 130; forced termination is 137. A pipe-only caller without an attached console may be unable to deliver CTRL_BREAK and still uses the bounded grace/force path. No PID from disk is signalled.

Direct file streams preserve console attachment. Non-file streams use bounded pipe drainage. Caller-owned streams remain open; as with os/exec, arbitrary caller-supplied Readers/Writers must make progress or provide their own interruption. This does not change the separate passphrase-console contract, which retains no blocked input reader. Windows SYSTEMROOT behavior matches os/exec, including explicit empty overrides; selected PATH and W3 argv/batch semantics remain intact.

Managed preflight checks the selected environment before port probes and repeats after binding expansion; readiness and the actual managed runner check again. Direct native wsl.exe and WSL Win32 PE roots are rejected. Foreground interop stays allowed. Arbitrary scripts can create cross-OS descendants: those trees remain unsupported and are not a claimed capability of the Job backend. Linux/macOS foreground execution and Unix TTY/job control stay unchanged.

The started callback allows W4D's supervisor to publish a successful actual start while this layer retains Job ownership. Detached supervisor/daemon integration is W4D; it remains explicitly unsupported in this intermediate build.

## Actual checks

- [Actions 39009037181](https://github.com/jinyongp/devtools/actions/runs/39009037181), exact reviewed commit: both native CPUs PASS. Each has **28 process test rows** (including fixture entrypoints) and **5 execution/doctor rows**, zero skip/failure. Actual Node/npm/pnpm and PS5.1/PS7/cmd cases still run.
- Real hidden-console three-level trees exit on CTRL_BREAK with wrapper 130, ignore it until Job termination with 137, and leave no surviving observed process handles. Early root exit preserves 23, reaps descendants and releases inherited output pipes. Owner process TerminateProcess closes the only Job handle and reaps all three descendants.
- Actual kernel Job flags include kill-on-close and exclude both breakaway flags. A real breakaway child creation fails. An unlisted inheritable event does not reach the child. Closed stdio cannot be mistaken for a pseudo process handle; 20 invalid-image starts do not leak handles.
- Start-publication error and panic both reap the already-started three-level tree. Callback cleanup is idempotent across Job/process/stream ownership. Stream failure setup cleanup retains the original resource pointer even when returning nil.
- Native managed wsl.exe is refused before any port probe/state creation; a renamed controlled fixture still runs in foreground. Linux PE/symlink fixtures reject managed Win32 roots and allow Linux executables. Guards use the selected injected/binding PATH and repeat after resolution and at execution/readiness.
- Native prior gates remain passing: 194 storage recovery, 80 storage integration, 40 full ports, 34 filesystem/path, 3 setup, 2 listener and 10 passphrase-console rows per CPU. Real packages pass linked identity and runtime version checks. Windows code/test binaries compile for both CPUs and Windows vet passes without local Windows execution.
- Unix full Go/vet and process/execution/services/CLI/proxy race suites PASS. The corrected deadline test passes ten focused repetitions and detects an intentionally stale anchor through a temporary Go overlay. W4P-unix contains raw results. Original failed full run and its 20 successful focused diagnostic repetitions were investigated before changing the fixture; no production proxy timeout changed.

## Last review loop

| Area | State | Findings, fixes and rechecks |
| --- | --- | --- |
| Atomic Job ownership | reviewed | JOB_LIST is attached in CreateProcess; no AssignProcessToJobObject after start. Kernel flags, actual breakaway rejection, nested trees and owner crash pass on both CPUs |
| Handle/stream ownership | reviewed | Three file stdio handles only; invalid/pseudo/non-file descriptors rejected. Original handles stay caller-owned. Fixed setup defer pointer and idempotent cleanup; live inherited-event exclusion and invalid-image handle-count checks pass |
| Cancellation and normal exit | reviewed | Poll whole Job accounting, not only the direct child. CTRL_BREAK grace 130, forced 137 and early exit 23 all pass with descendant/process-handle and EOF checks |
| Start failure and panic | reviewed | Publish-start hook error/panic cannot leave a child tree or unclosed owned resources; actual three-level failure fixtures pass |
| W3 argv/env compatibility | reviewed | Complete native W3 shell/shim/Unicode/streams cases pass. Critical SYSTEMROOT matches os/exec, explicit empty override preserved. Old 100 ms single-child cancel fixture is replaced by pre-create cancellation plus stronger real live grace/force tree cases |
| Managed bridge preflight and callers | reviewed | Selected profile/PATH checks precede probes and repeat after bindings. CLI/execution/readiness call sites rechecked. Direct bridges are refused, arbitrary cross-OS script descendants remain unsupported |
| Unix compatibility and meaningful checks | reviewed | No Unix TTY/job-control source change. Full tests/vet and scoped race pass; fixed a pre-existing timing-only test assumption with direct persisted-clock protection and a failing stale-clock mutation |
| Evidence and boundaries | reviewed | Exact native commit, actual runner metadata, no skipped required rows. Physical PC UI/terminal tests are not claimed; detached supervisor/daemon integration remains W4D |

All W4P areas reviewed; no eligible remaining finding or required unexecuted W4P check. This does not complete W4D, remaining Windows features, final product gates or W9.

Sources: [Job Objects](https://learn.microsoft.com/en-us/windows/win32/procthread/job-objects), [process creation attributes](https://learn.microsoft.com/en-us/windows/win32/api/processthreadsapi/nf-processthreadsapi-updateprocthreadattribute), [console group events](https://learn.microsoft.com/en-us/windows/console/generateconsolectrlevent). GOROOT src/syscall/exec_windows.go and src/os/exec/exec.go provide the existing Go argv/environment/handle conventions.

## Unix verification fixture correction

The full Unix gate intermittently failed TestManagerStartsWithFreshDeadlineAfterTransitionLockContention, also observed before W4P. Its 100 ms budget included real filesystem/RPC startup. Product code already creates the startup anchor after acquiring the transition lock. The test now synchronizes on persisted request intent, asserts the start remains blocked while the lock is held, checks the persisted CreatedAt is after release, and retains a real start/stop flow with a normal startup budget. A Go overlay that deliberately moves CreatedAt before lock acquisition fails the new timestamp assertion. No product timeout or production proxy behavior changed. Ten focused repetitions, full Unix Go/vet and affected race checks pass after this test correction.
