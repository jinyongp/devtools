# W4P — Windows owned process launcher

Task: `ada0f70c-2e8c-478b-9da6-42ad135131e0`. Status: implementation pending native Actions; not complete.

## Boundary

CreateProcess receives PROC_THREAD_ATTRIBUTE_JOB_LIST and a non-inherited kill-on-close Job at creation. There is no start-then-assign interval. Only duplicated file stdio handles enter the application HANDLE_LIST; the separate supervisor lease handoff belongs to W4D; invalid/pseudo/non-file stdio is rejected. Job breakaway is disabled. The parent owns process/thread/job/pipe handles and cleans each failure path, including a rejected or panicking start-publication callback.

Normal direct-child exit retains its exit code and closes the Job before draining inherited pipes, cleaning descendants that outlive the root. Cancellation sends CTRL_BREAK only to the created child group, waits up to three seconds for the owned Job to become empty, then terminates the Job. Wrapper cancellation is 130; forced termination is 137. A pipe-only caller without an attached console may be unable to deliver CTRL_BREAK and still uses the bounded grace/force path. No PID from disk is signalled.

Direct file streams preserve console attachment. Non-file streams use bounded pipe drainage. Caller-owned streams remain open; as with os/exec, arbitrary caller-supplied Readers/Writers must make progress or provide their own interruption. This does not change the separate passphrase-console contract, which retains no blocked input reader. Windows SYSTEMROOT behavior matches os/exec, including explicit empty overrides; selected PATH and W3 argv/batch semantics remain intact.

Managed preflight checks the selected environment before port probes and repeats after binding expansion; readiness and the actual managed runner check again. Direct native wsl.exe and WSL Win32 PE roots are rejected. Foreground interop stays allowed. Arbitrary scripts can create cross-OS descendants: those trees remain unsupported and are not a claimed capability of the Job backend. Linux/macOS foreground execution and Unix TTY/job control stay unchanged.

The started callback allows W4D's supervisor to publish a successful actual start while this layer retains Job ownership. Detached supervisor/daemon integration is W4D; it remains explicitly unsupported in this intermediate build.

## Verification and review ledger

| Area | State | Required evidence |
| --- | --- | --- |
| Job at creation and handle ownership | needs-recheck | Native nested tree, parent crash, kernel Job flags, breakaway and stdio/unlisted inheritable handle fixtures |
| Cancellation, exit and pipe EOF | needs-recheck | Real hidden-console graceful/forced three-level trees, normal early-root exit and whole-tree cleanup |
| Start/failure cleanup | needs-recheck | Invalid image repeated handle count, closed stdio pseudo-handle rejection and start-publication failure tree cleanup |
| Argv, environment and streams | needs-recheck | Complete existing W3 tests, critical SYSTEMROOT behavior and actual shell/package shims |
| Managed interop and callers | needs-recheck | Windows bridge preflight before port probes, foreground allowance, Linux PE/symlink fixture, readiness/CLI/execution callers |
| Unix compatibility | needs-recheck | Complete Go/vet and affected race checks; retain Unix TTY/job-control tests |
| Final task | pending | Both native CPUs and final scoped review before result/done |

Sources: [Job Objects](https://learn.microsoft.com/en-us/windows/win32/procthread/job-objects), [process creation attributes](https://learn.microsoft.com/en-us/windows/win32/api/processthreadsapi/nf-processthreadsapi-updateprocthreadattribute), [console group events](https://learn.microsoft.com/en-us/windows/console/generateconsolectrlevent). GOROOT src/syscall/exec_windows.go and src/os/exec/exec.go provide the existing Go argv/environment/handle conventions.

## Unix verification fixture correction

The full Unix gate intermittently failed TestManagerStartsWithFreshDeadlineAfterTransitionLockContention, also observed before W4P. Its 100 ms budget included real filesystem/RPC startup. Product code already creates the startup anchor after acquiring the transition lock. The test now synchronizes on persisted request intent, asserts the start remains blocked while the lock is held, checks the persisted CreatedAt is after release, and retains a real start/stop flow with a normal startup budget. A Go overlay that deliberately moves CreatedAt before lock acquisition fails the new timestamp assertion. No product timeout or production proxy behavior changed. Ten focused repetitions, full Unix Go/vet and affected race checks pass after this test correction.
