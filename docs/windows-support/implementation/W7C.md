# W7C — public CLI and PowerShell completion

Code basis `aecadca1f32edae512d4d6c74dc2f06d460b561e`, definition136.
[Actions39020361228](https://github.com/jinyongp/devtools/actions/runs/39020361228)
passed on both native runners. Each has 11 CLI + 9 complete profile-catalog Go
passes, no fail/skip, and actual PS5.1/7.6.6 completion checks.
[Raw native results](W7C-actions-39020361228/) and [Unix checks](W7C-unix/)
retain the observed metadata and test output.

Protocol7 adds diagnostics.platform and powershell completion; envelope1 and
versionless TOML remain unchanged, including the v6 run/child argument boundary.
All public Windows commands now use their native backends. The private preflight
retains an explicit native allowlist and refuses obsolete Unix helper/install
entry points before reading a store. Existing special native installer and network
helpers keep their constrained entry handlers before that preflight.

PowerShell uses CLI metadata for static candidates and the existing bounded
__complete query for dynamic names. Only literal AST values are sent as UTF-8
NUL-delimited stdin. Expressions are never evaluated; quote escaping uses doubled
single quotes. Framework stdin BOM avoidance is scoped to child Start and restores
console encoding. The explicit filesystem WorkingDirectory follows PowerShell's
location. Registration is Native for devtools/dvt and .exe names. cmd remains
execution-only; Unix completions retain their behavior.

Review findings corrected and rechecked:

- PS5 dynamic queries initially used the process directory instead of PowerShell's
  current location. Explicit WorkingDirectory fixes it; actual Korean/space/quote,
  colon names, cursor-prefix and independent expression canary cases now pass.
- The first smoke used a removed schema.arguments mirror; it now checks the actual
  public input_schema. This was a fixture defect, not a product schema change.
- CLI/doctor fixtures used chmod instead of Windows DACLs. SDK-created private
  fixture roots preserve the security requirement; no validator was relaxed.
- The run boundary fixture used /bin/sh. Windows now executes a native Go child
  and preserves every existing empty/Unicode/option/help/alias case. Unix retains
  its actual shell fixture.
- The profile catalog still inspected POSIX bits, rejecting valid Windows stores.
  It now uses DirectoryPrivate; all nine catalog/diff contracts and actual CLI
  profile list + diagnostics ready=true pass. Insecure Windows fixtures use a real
  public ACE, while raw legacy/corrupt fixtures retain private ACLs.
- Exposure auth-file still used the unavailable OpenNoFollow boundary. It now uses
  the external private-file SDK with CheckFile, preserving public parents and
  rejecting an insecure leaf. Native positive/negative cases pass. Proxy cache
  identity/directory checks use the same SDK instead of an unconditional Windows
  cache fallback caused by POSIX bits.

Final review loop, performed after the native results:

| Area | Final state and evidence |
| --- | --- |
| AST/token/quoting/privacy | Reviewed; literal-only extraction, bounded NUL query, no shell interpolation, restored encoding, actual PS5/7 UTF-8 and quote cases, independent non-evaluation canary pass. |
| Cursor/child argv/help | Reviewed; mid-token prefix ignores later argv; run/command run/cmd run preserve child tokens and help. Static and dynamic candidates stop after the child name. Native fixture and PS smoke pass. |
| Public/private surface and ACL callers | Reviewed; public backends exposed, known native private entries allowlisted, Unix/unknown entries denied. Auth file, profile catalog and cache callers rechecked after fixes; native privacy/permission cases pass. |
| Protocol/schema/output | Reviewed; protocol7, envelope1, artifact output, public shell pattern, platform schema and versionless TOML match runtime. Existing v6 run semantics retained. |
| Unix regressions | Reviewed; final full Go suite/vet pass, profile/CLI race pass. Portable Node tooling has 12 passes; Skill validation+seven release tests pass (those source files unchanged since their checked basis). |
| Reader docs/Agent Skill | Reviewed; both READMEs, Windows/install/completion/local-network guides, WSL link, protocol migration and source Skill agree about candidate/unpublished state and accepted support limits. CurrentUser interactive OS consent, storage/batch/PS5/provider/daemon/CA boundaries remain explicit. |
| Workflow/evidence/candidate | Reviewed; full push/PR gates retained, focused completion does not claim full-product success. Both CPU raw records inspected; exact immutable source fixed for W8A. |

No unresolved eligible defect remains in the W7C completion/public-contract scope.
Full-product integration, packaged browser and release-gate validation remain
required W8A/W9 work; no release/tag/main merge occurred.

Integration handoff: earlier full runs (e.g.39019216181) intermittently failed
project up with generic private process-storage errors. Safe operation/OS-error
metadata was added without raw paths/records/argv/credentials. A diagnostic-only
[39019811636](https://github.com/jinyongp/devtools/actions/runs/39019811636), source
`4a5d20a5a8b456fcdce03cbcceb63f18c4e32ba7`, passed 20 repeats per native CPU.
This does not establish a root-cause fix. W8A must retain full lifecycle checks,
investigate any recurrence, and carry the historical uncertainty into total review;
these repeats are not represented as proof that a diagnosed defect was fixed.
Real-PC UAC/browser/terminal and WSL2 browser networking remain NOT RUN under the
accepted Actions-only policy. The runner Chromium gate is still mandatory in W8A.

Primary completion reference:
https://learn.microsoft.com/en-us/powershell/module/microsoft.powershell.core/register-argumentcompleter?view=powershell-7.6
