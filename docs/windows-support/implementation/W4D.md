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
