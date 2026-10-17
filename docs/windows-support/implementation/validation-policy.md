# Validation policy amendment — 2026-10-17

User decision: “Actions 검증으로 확정하고 실제 PC 미검증 한계 공개”.

This replaces the previous required Windows 11 x64 desktop eight-row gate. Continue Windows execution only on GitHub Actions. Required product gates cover native amd64/arm64 Actions and existing Unix regression checks at the same candidate revision/artifacts. Record actual runner OS/build and architecture; Server x64 does not prove Windows 11 x64 desktop compatibility.

W8L now reconciles CI evidence and publishes actual-PC NOT RUN limits. UAC prompt interaction, actual-PC browser/trust integration, terminal closure, actual-PC installation/update and final-product WSL2 networking remain unverified. Historical standalone local probes remain historical evidence. Linux fixtures cannot prove Windows/WSL networking. No Windows 10, self-hosted equipment or new local PC setup is required.

Task-by-task and final W9 review loops remain required. No automatic test may be removed or failed/missing required CI row relabelled as a pass. This scope amendment does not authorize a real release, tag or main merge.

## Revalidation of earlier tasks

The coordinator correctly marked previous completions stale when the shared specification changed. W0 was reviewed against the unchanged historical probe checksums and the amended disclosure policy. W1/W1L/W2S/W3/W3C/W7P are rechecked against [Actions 39006535054](https://github.com/jinyongp/devtools/actions/runs/39006535054), immutable code bcd22ebe75c032b1a1933c8b3f30eb8dd1dc2983; later policy changes affect documentation only. The current result files are under W2B-actions-39006535054.

| Scope | Current evidence and last review |
| --- | --- |
| W1 paths/private storage | 34 native rows per CPU, actual separate-user denial, compile/internal gate checks; W2B repairs the newly found held-leaf sharing defect and rechecks callers |
| W1L locks/leases | Native maintenance intent/shared/exclusive checks and real inherited lease exclusion/adoption cases pass, empty lease cleanup stays excluded |
| W2S task/value/profile recovery | All 194 storage/recovery rows per CPU pass, including abrupt process death and large before-image recovery; power-loss durability remains outside claims |
| W3 argv/env | All 11 native process and 4 doctor/execution rows pass with actual shells and npm/pnpm shims; conservative batch domain and PS5.1 limits unchanged |
| W3C passphrase/console | All 10 console rows pass; native mode restoration, Unicode, cancellation and redirected input reviewed without claiming an actual user's terminal/UI observation |
| W7P portable tooling/package | Both CPUs pass Node/Python/Skill/Go identity and actual native package checks; Linux statically verifies both downloaded ZIPs, without executing Windows images locally |

Each earlier task retains its detailed area review in its own document. This amendment rechecks changed policy, evidence applicability, remaining supported omissions and affected native private-I/O callers; no optional capability becomes a new requirement. Linux full tests/vet and affected race checks also pass. Task result and final review records are renewed on the current definition rather than treating stale completions as current.
