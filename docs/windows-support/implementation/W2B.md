# W2B — Backup, transfer, registry, logs and cleanup

Task: `b220707b-ca42-4fae-8096-a9c452b0f636`. Reviewed code: `bcd22ebe75c032b1a1933c8b3f30eb8dd1dc2983`. Result: W2B checks and last review PASS.

## Scope and decisions

Private state retains checked roots and owner/SYSTEM security. External encrypted archives, recipient files and private keys may use a user-selected existing public parent: the file remains private, ancestors remain pinned and reparse/remote paths are rejected, while parent ACLs are never repaired. Unix file modes, no-follow opens, link publication and directory sync remain in Unix backends.

Windows pending transfer identity publication uses a no-replace WRITE_THROUGH move. Coexisting target/pending images are ambiguous rather than a valid link-publication intermediate state; preserve them and reject. Existing preview, digest, replay, safety archive, logical task/value snapshots and host-local active-ownership exclusions remain shared.

Windows registry replacement and private log/marker deletion use checked native operations. Retained empty process leases are excluded from cleanup. Socket occupancy detection is W5N; this task's ports checks cover registry/storage semantics, not the full networking backend.

## Recovery of development state

The previous `/tmp` worktree and uncommitted W2B files were absent when work resumed. Committed work through 678e055 and the coordinator survived. W2B was reconstructed from the recorded design and rechecked in a persistent repository-local worktree. Previous uncommitted test logs are unavailable and do not count as evidence for this reconstruction. Local Windows execution stopped; subsequent Windows execution is GitHub Actions only.

## Actual checks

- [Native Actions 39006535054](https://github.com/jinyongp/devtools/actions/runs/39006535054), exact reviewed commit: amd64 and arm64 PASS. Each CPU has **80 storage integration test/subtest rows**, **26 registry storage rows**, and no skip/failure. Complete backup, transfer, cleanup, archiveproof and services suites run natively.
- Each retains 194 complete task/value/profile/maintenance recovery rows, 34 filesystem/path rows (including the direct held-root rename regression), 11 process rows, 4 execution/doctor rows and 10 console rows. Native lock/lease and package checks also pass; counts are per result file rather than a sum of deduplicated tests.
- External archive/private-key reads and no-replace writes keep an existing public parent ACL unchanged. Cleanup's old-backup path was also reviewed: scan, retirement and restore now use that external boundary. A native four-backup fixture archives/restores the aged backup and compares the parent security descriptor before/after.
- Pending transfer recovery, real valid-key public-ACL rejection without repair, hardlink rejection/preservation, stale digest, same-request replay, passphrase and recipient archives, private safety recovery, large import receipt, registry concurrency, log compaction/retirement/restoration, archive proof and retained empty lease cases pass on both CPUs.
- Linux full `go test ./...` and `go vet ./...` PASS. Affected platformfs/backup/profiletransfer/cleanup/archiveproof/services/ports race suites PASS. After the cleanup external-source correction, its suite/vet/race pass again. Both CPU test binaries cross-compile; none of these Windows images run locally.
- Native Go/Node/Python project/package/skill checks and real ZIP/runtime identity checks pass on both CPUs. ZIP bytes remain in the Actions artifact; checksums and static PE/Go identity inspections are in the saved evidence. Linux inspection never executes a Windows image.
- Run 39006420655 was superseded and canceled after the cleanup review fix; it is not passing evidence. Downloaded JSONL/TAP/JSON has normalized line endings/trailing whitespace only.

## Last review loop

| Area | State | Findings, fixes and rechecks |
| --- | --- | --- |
| Private state versus external artifacts | reviewed | Checked root semantics retained; external files have explicit private security with pinned local/reparse-free ancestors, while existing public parents are never repaired. Native ACL/no-repair tests pass |
| Directory identity and sharing | reviewed | Metadata-only directory guards failed to pin the held leaf. FILE_LIST_DIRECTORY fixes sharing participation; direct root-rename and prior ancestor/file-retained guard cases pass on both CPUs |
| Publication and deletion | reviewed | Retry ACL/identity source/target checks as well as MoveFileEx on transient sharing/access failures. Exclusive target reader, cancellation, delayed close, directory publication and deletion tests pass. No Windows directory-fsync success stub |
| Backup and transfer | reviewed | Unix algorithms retained in their backend. Windows single-move pending publication rejects ambiguous coexistence. Preview/digest/stale/replay/secret exclusion/safety archive and invalid ACL/hardlink paths pass |
| Registry and log callers | reviewed | Private handle/DACL checks replace Windows POSIX modes; checked registry replacement and log/marker deletion preserve locking, size limits and logical tails. Full native services and 26 registry storage rows pass |
| Cleanup and archive proof | reviewed | Fixed external backup scan/retire/restore; actual public-parent round trip passes. Native archive directory ACL checks preserve process proof; retained empty leases are not selected for GC |
| Readiness fixture and consumers | reviewed | POSIX shell checks stay in Unix tests. Native self-executable verifies cwd/environment/exit/timeout/ended-record behavior; tree cancellation remains W4P |
| Unix and affected callers | reviewed | Full Unix tests/vet and affected race suites pass; no file-format or journal/replay contract change. Existing Unix directory-sync operations preserved |
| Evidence and scope | reviewed | Both native CPUs pass immutable reviewed code without skips. Windows execution is Actions only. W5N socket availability, W4 lifecycle and final product/public gates remain separate tasks |

All W2B areas reviewed; no eligible in-scope finding or required unexecuted W2B check remains. This does not complete W4-W8 or the final W9 review.
