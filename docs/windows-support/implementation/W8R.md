# W8R — final candidate/public-contract acceptance

Definition183; immutable candidate `50ccb27b5df2d47ad56a1b24570dde66b9e7a2ba`, validation version
`0.0.0-windows-candidate`. [Full release validation](https://github.com/jinyongp/devtools/actions/runs/39024924795) passes
both native Windows full jobs, Mac/Linux and package. Publication jobs are skipped.
[W8A](W8A.md) and [exact validation limits](../evidence/product/50ccb27b5df2d47ad56a1b24570dde66b9e7a2ba/validation-limits/README.md)
bind the actual source, both native ZIPs, release-assets and runner/browser metadata.
[Machine handoff](W8R-candidate.json) retains the same identity and canonical Skill hash.

Final acceptance readback:

- Both READMEs identify native Windows as a next-release candidate, not published
  stable assets. Windows11 25H2/26H2 x64/ARM64 targets, Server2025 test-host role,
  unsupported Windows10/other excluded editions and actual-PC/WSL2 limits agree.
- Windows guide, installer/update source and release assets agree on standalone
  exe, owned dvt copy, Known Folder/private NTFS ACLs, explicit PATH changes,
  native OS architecture under x64 PowerShell on ARM64, journal/recovery and matching
  source/version/checksum. Real PS5/7 update/fault gates and actual candidate install pass.
- Source Skill, CLI metadata/help/schema, output guide and actual native contracts
  agree on protocol7, envelope1, versionless TOML, PowerShell artifact output and
  unchanged v6 run/child boundary. Actual PS5/7 dynamic/static/literal/Unicode and
  non-evaluation checks pass. cmd remains execution-only and PS5 argv limits stay explicit.
- WSL guide matches diagnostics.platform and Linux storage/tool lookup, independent
  Windows/WSL daemon/data/CA/active ownership, explicit WSLENV opt-in and public-CA-only
  export/fingerprint/client trust guidance. No automatic firewall/portproxy/WSL config
  mutation or final WSL2/browser product PASS is claimed.
- Contributor/install/verification docs agree with full native gates, amd64 race vs
  ARM contention, actual Chromium CPU metadata, six artifacts and native-tested ZIP
  reuse. Skill validation/archive and local/public discovery pass in final Unix jobs;
  package compares Mac/Linux Skill bytes and verifies all six identities/checksums.
- Pages source uses the selected stable release tag and byte-matches each installer
  present in that tag; historical Unix-only tags do not invent a Windows asset from
  main. publish=true requires main ref/ancestry; publish=false leaves tag/Release/
  Homebrew/Pages untouched. Neither this task nor this workstream publishes them.
- Read back18 predecessors as done/current. Their last-review results are retained;
  W8A supplies final integrated native/Unix evidence for the unchanged contracts and
  its SDK/cancellation/probe corrections. W8L supplies the exact same ZIP/limit boundary.
  W9 and final workstream acceptance remain required before closure.

## Last review loop

| Area | State | Evidence/conclusion |
| --- | --- | --- |
| User support/install examples | Reviewed | READMEs, Windows/install guide and actual candidate installer/runtime agree; candidate/unpublished and excluded/NOT RUN scope preserved. |
| Public machine/Skill/completion contract | Reviewed | Actual native version/schema/help/PS5/7, source Skill validation and release payload match protocol7/envelope1/versionless TOML and v6 child semantics. |
| WSL and trust/ownership boundaries | Reviewed | Public docs and diagnostics fixture contracts match; no cross-OS active-state or CA-private-key transfer, no unperformed product integration claim. |
| Release/Pages/artifact identity | Reviewed | Same full SHA and native ZIP hashes verified against release-assets; six archive and Skill checks pass, publication guard/selected-tag source retained. |
| Task/validation handoff | Reviewed |18 predecessors current, W8R result and last review recorded after this readback. W9 receives the exact candidate and complete evidence; no premature workstream close. |
| Corrections/recheck boundary | Reviewed | W8A already included the diagnosed fixture/doc corrections before final candidate execution. This task adds only acceptance records, with no change to candidate code, public usage docs or packaged inputs. |

No eligible W8R mismatch remains. This acceptance is complete within the agreed
Actions-only policy; actual-PC UAC/trust/browser/terminal/install and final WSL2
networking remain NOT RUN. Final whole-change review/closure is W9.
