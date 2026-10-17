# W5E — provider transports and process lifetime

Status: implementation prepared; native Actions validation and final review pending.

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

Review ledger: transport/namespace, API errors/concurrency, CLI output/ownership,
cloudflared spawn/lifetime/recovery, Unix callers, workflow/evidence are pending
final review after the native checks. No completion claim yet.
