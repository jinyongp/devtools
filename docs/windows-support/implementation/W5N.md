# W5N — Native Winsock availability and setup

Task: `481f792a-0096-466d-b993-ea2f136a8fe4`. Status: implementation ready for native Actions; not complete.

## Contract and implementation

Windows availability and owned proxy TCP listeners set SO_EXCLUSIVEADDRUSE before bind. Probe IPv4/IPv6 wildcard and loopback addresses, close each probe and retain no availability reservation. Actual listener startup must bind again; a successful probe never guarantees a later bind.

Winsock errors use their native codes. WSAEADDRINUSE means occupied; WSAEACCES is a typed permission/reservation denial and can never become free or an unavailable-family skip. Unsupported protocol/address families stay distinct. Proxy listener failures use the same classification. Unix keeps its prior socket policy, errors and Darwin-specific availability behavior.

Native setup reads stable NTFS executable identity through a handle that excludes write/delete while held, with volume/file identity and digest. It journals intent and reports native mode; no Linux capabilities, firewall, portproxy or excluded-range setting is changed. Actual listener bind remains authoritative. Unix capability fixtures remain Unix tests; native tests exercise native intent/replay/identity/isolation and refusal of capability mutation.

## Evidence boundary

The real WSAEACCES fixture attempts SO_REUSEADDR against a live exclusive listener. It reproduces the access-denied socket error class without creating/removing an OS excluded range. It does not claim to have modified or exhaustively tested every system reservation policy. The 16-way real listener race must have exactly one owner. The proxy fixture reads the kernel exclusive-bind option on actual loopback sockets.

Sources: [Microsoft socket option semantics](https://learn.microsoft.com/en-us/windows/win32/winsock/using-so-reuseaddr-and-so-exclusiveaddruse), [Winsock error codes](https://learn.microsoft.com/en-us/windows/win32/winsock/windows-sockets-error-codes-2). These define the expected runtime behavior; only actual native Actions can verify this implementation.

## Review ledger

| Area | State | Next check |
| --- | --- | --- |
| Availability, socket options and concurrency | needs-recheck | Full native ports suite and real exclusive/denied/concurrent fixtures on both CPUs |
| Proxy listener wiring and typed failures | needs-recheck | Native kernel option and native error-code tests; Unix full proxy suite/race |
| Native setup identity and no system mutation | needs-recheck | Both CPU native setup suite, intent retry and held-image checks |
| Unix compatibility and callers | needs-recheck | Relevant suites pass; final vet/race/cross-build/native evidence pending |
| Final result | pending | Native Actions plus last review required before done |
