# W5N — Native Winsock availability and setup

Task: `481f792a-0096-466d-b993-ea2f136a8fe4`. Reviewed code: `140b49f9756fe0cb4e1e5d775e05daf9d35403fb`. Result: native checks and last scoped review PASS.

## Contract and implementation

Windows availability and owned proxy TCP listeners set SO_EXCLUSIVEADDRUSE before bind. Probe IPv4/IPv6 wildcard and loopback addresses, close each probe and retain no availability reservation. Actual listener startup must bind again; a successful probe never guarantees a later bind.

Winsock errors use their native codes. WSAEADDRINUSE means occupied; WSAEACCES is a typed permission/reservation denial and can never become free or an unavailable-family skip. Unsupported protocol/address families stay distinct. Proxy listener failures use the same classification. Unix keeps its prior socket policy, errors and Darwin-specific availability behavior.

Native setup reads stable NTFS executable identity through a handle that excludes write/delete while held, with volume/file identity and digest. It journals intent and reports native mode; no Linux capabilities, firewall, portproxy or excluded-range setting is changed. Actual listener bind remains authoritative. Unix capability fixtures remain Unix tests; native tests exercise native intent/replay/identity/isolation and refusal of capability mutation.

## Evidence boundary

The real WSAEACCES fixture attempts SO_REUSEADDR against a live exclusive listener. It reproduces the access-denied socket error class without creating/removing an OS excluded range. It does not claim to have modified or exhaustively tested every system reservation policy. The 16-way real listener race must have exactly one owner. The proxy fixture reads the kernel exclusive-bind option on actual loopback sockets.

Sources: [Microsoft socket option semantics](https://learn.microsoft.com/en-us/windows/win32/winsock/using-so-reuseaddr-and-so-exclusiveaddruse), [Winsock error codes](https://learn.microsoft.com/en-us/windows/win32/winsock/windows-sockets-error-codes-2). These define the expected runtime behavior; only actual native Actions can verify this implementation.

## Actual checks

- [Actions 39008060871](https://github.com/jinyongp/devtools/actions/runs/39008060871), exact reviewed commit: both native CPUs PASS. Each has **40 complete ports test/subtest rows**, **3 native setup rows**, and **2 proxy listener rows**, with zero skip/failure. The previously filtered ports suite now runs in full.
- Actual wildcard/specific IPv4/IPv6 listener conflicts, 16-way exclusive ownership, real WSAEACCES from a protected listener and wrapper error classification pass on both CPUs. The kernel reports exclusive mode on actual proxy loopback sockets. No global exclusion/firewall/portproxy setting is changed.
- Native setup retries the same request through a lowercase alias of the same executable, retains the same identity/digest, and never invokes a capability helper. Held image write/rename exclusion, replacement digest, isolated guard and native capability mutation refusal pass.
- Current runner metadata is saved: x64 Microsoft Windows Server 2025 Datacenter build 26100 (display 24H2), ARM64 Windows 11 Enterprise build 26200 (25H2), both elevated; PowerShell 7.6.6. This is not actual-PC or ordinary-token product validation.
- All existing native gates remain passing: 194 storage recovery, 80 storage integration, 34 filesystem/path, 11 command process, 4 doctor/execution and 10 console rows per CPU; package/Skill/Node/Python checks and native ZIP/runtime identity pass. Per-file counts include overlapping focused tests and must not be summed as unique tests.
- Unix full Go/vet and scoped ports/proxy/netsetup race checks pass at the initial W5N code. After the final Windows identity-alias correction, netsetup tests/vet and ARM64 test compilation pass; its Unix adapter preserves the exact prior EvalSymlinks behavior. Both native CPUs recheck the final correction.
- Initial Actions 39007379948 passed on both CPUs before the review found the path-alias replay defect. Only final run 39008060871 passes that correction. Later uncommitted W4P launcher/interop changes are excluded from this immutable W5N result.

## Last review loop

| Area | State | Findings, fixes and rechecks |
| --- | --- | --- |
| Availability and socket ownership | reviewed | Exclusive option is set before actual bind. Both wildcard/specific families and real competing listeners pass; probe handles close and do not claim a future reservation |
| Error classes and denial | reviewed | Fixed emulated Unix errno comparisons on Windows. Real denied bind stays permission_required; occupied and unsupported-family errors stay distinct, with no false-free result |
| Proxy callers | reviewed | Shared listener creation and native failure classification wired to actual proxy sockets. Native kernel option/diagnostic checks and Unix proxy suite/race pass |
| Native setup identity | reviewed | Stable handle identity/digest, no write/delete during inspection. Final review found EvalSymlinks spelling could change a same-request fingerprint; Windows now uses canonical handle identity and native lowercase-alias replay passes |
| Setup intent and system limits | reviewed | Native mode journals intent, never calls Linux capability helper or changes network settings. Real bind remains authoritative; isolation/retry and unsupported native capability mutation tested |
| Unix and compatibility | reviewed | Prior socket policy, Darwin-specific probes and Unix identity/capability semantics preserved. Existing shared consumers reviewed; full Unix/vet and focused race pass before the Windows-only identity correction |
| Evidence and support claims | reviewed | Exact final native commit, architecture and actual elevated runner OS recorded. Actual-PC/standard-token assumptions are not substituted for Actions evidence; physical excluded-range changes were not performed |

All W5N areas reviewed; no remaining eligible finding or required unchecked W5N gate. W4 process ownership, W5H hosts/trust/helper, final product gates and W9 remain separate work.
