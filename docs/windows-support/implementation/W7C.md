# W7C — public CLI and PowerShell completion

Implementation prepared. Native PS5.1/7 checks and final review are pending.
Protocol7 adds diagnostics.platform and powershell completion; envelope1 and
versionless TOML remain unchanged, including the v6 run/child argument boundary.
All public Windows commands now use their native backends. The private preflight
retains an explicit native allowlist and refuses obsolete Unix helper/install
entry points before reading a store.

PowerShell uses CLI metadata for static candidates and the existing bounded
__complete query for dynamic names. Only literal AST values are sent as UTF-8
NUL-delimited stdin. Expressions are never evaluated; quote escaping uses doubled
single quotes. The Framework stdin BOM behavior is avoided for the child's Start
scope only, then console encoding is restored. Registration is Native and supports
devtools/dvt and .exe names. cmd remains execution-only. Unix completions remain.

README, Windows/install/completion/local-network guides, protocol migration and
Agent Skill distinguish source candidate vs published release, actual runner vs
support OS, CurrentUser interactive consent, real-PC/WSL/provider gaps, local NTFS,
batch/PS5 argv and daemon/CA/data boundaries. No release/tag/main merge is implied.

Pending review areas: AST/token/quoting/privacy, child boundary and dynamic input,
public/private command surface, protocol/schema/output, Unix consumers, user docs
and Skill, workflow/evidence and final candidate identity. Finish only after all
required checks apply to the final source state.
