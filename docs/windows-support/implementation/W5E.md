# W5E — provider transports and process lifetime

Status: implemented and reviewed. Code basis
`a8a638a5c64292793b0c1b3649ab4176d3c4247c`; task definition revision 136.
[Actions 39018820589](https://github.com/jinyongp/devtools/actions/runs/39018820589)
passed on both native runners. Each emitted 35 passing Go rows and zero fail/skip
rows. The x64 ARM-emulation case is explicitly inapplicable on that host and
logs that fact; actual amd64-on-ARM execution ran on the ARM64 runner.
Raw runner metadata and results are in [W5E-actions-39018820589](W5E-actions-39018820589/).
Linux affected-suite race and five-repeat checks passed; both Windows
architectures cross-compiled and passed vet. No Windows binary ran locally.

Windows Tailscale requires 1.104.1 or newer and connects only to
`\\.\pipe\ProtectedPrefix\Administrators\Tailscale\tailscaled`.
The byte-stream HTTP transport uses overlapped handles, identification SQOS,
10-second bounds, cancellation, no proxy, and no redirects. Missing/denied
endpoints fail immediately; only busy instances retry within the deadline.
`debug local-creds` is never invoked. ETag/If-Match and future configuration
fields retain the common API contract; no install/login or ownership adoption.
The status CLI runs inside an owned Job and has bounded output.

Cloudflared remains user-installed. Its hidden supervisor owns a separate console
and the provider Job, with no breakaway. Lifeline EOF/cancellation ends the tree;
supervisor crash closes the Job. Recovery accepts only the owned stopped receipt,
never disk PIDs. ARM64 support uses the user-installed amd64 executable under
Windows emulation. The fixture records devtools and provider architectures
separately. Actual vendor accounts, public connectivity, and real-PC UI remain
unverified; fixtures do not establish those claims.

The common Start extraction preserves Unix launch/signals, with one corrected
race: an already-exited provider whose scanners observed a URL must report
`cloudflared_exited`, even when the completion and URL channels become ready
together. It must not randomly report that no URL was observed.

Primary source basis, pinned Tailscale v1.104.1:

- https://github.com/tailscale/tailscale/blob/v1.104.1/paths/paths.go
- https://github.com/tailscale/tailscale/blob/v1.104.1/safesocket/pipe_windows.go
- https://github.com/tailscale/tailscale/blob/v1.104.1/client/local/local.go
- https://github.com/tailscale/tailscale/blob/v1.104.1/client/local/serve.go

Final review loop (performed after both native results):

| Area | Final state and evidence |
| --- | --- |
| Fixed namespace, overlapped/deadline handles, cancellation | Reviewed; actual HTTP pipe, missing/busy/unprotected endpoints, canceled request and deadline all passed. Returned connection belongs to HTTP Transport, not the short-lived dial context. |
| API concurrency and errors | Reviewed; ETag/If-Match stale update, unknown field preservation, 401/403, redirect refusal, bounded response and invalid JSON passed. Existing shared ownership/rollback tests remain enabled. |
| CLI lookup/version/output ownership | Reviewed; minimum 1.104.1, no credential proxy, actual native status process, failure/cancel/output bounds passed; no automatic install/login. |
| Cloudflared spawn/stop/recovery | Reviewed; owner journal before start, missing/bad executable, supervisor spawn failure, early exit/malformed URL, idempotent stop, cancellation, EOF with ignoring descendant, supervisor crash and stopped-receipt recovery passed. No disk-PID termination. |
| Unix extraction and callers | Reviewed; original process group/signal behavior and version minimum retained. Early-exit URL race corrected and both native suites plus five Unix repeats passed. |
| Workflow and evidence | Reviewed; providers focus skips unrelated installer/network gates; push/PR still run all full gates. Both native metadata and raw rows inspected. Provider emulation is distinct from native devtools architecture. |

No unresolved eligible finding remains in this task scope. Focused success is
not the final full-product CI claim; W8A/W9 retain that gate. Real Tailscale and
cloudflared accounts, LAN/public connectivity and real-PC UI are NOT RUN under
the accepted Actions-only policy.
