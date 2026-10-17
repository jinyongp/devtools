# W2B — Backup, transfer, registry, logs and cleanup

Task: `b220707b-ca42-4fae-8096-a9c452b0f636`. Status: in progress; native Actions pending.

## Scope and decisions

Private state retains checked roots and owner/SYSTEM security. External encrypted archives, recipient files and private keys may use a user-selected existing public parent: the file remains private, ancestors remain pinned and reparse/remote paths are rejected, while parent ACLs are never repaired. Unix file modes, no-follow opens, link publication and directory sync remain in Unix backends.

Windows pending transfer identity publication uses a no-replace WRITE_THROUGH move. Coexisting target/pending images are ambiguous rather than a valid link-publication intermediate state; preserve them and reject. Existing preview, digest, replay, safety archive, logical task/value snapshots and host-local active-ownership exclusions remain shared.

Windows registry replacement and private log/marker deletion use checked native operations. Retained empty process leases are excluded from cleanup. Socket occupancy detection is W5N; this task's ports checks cover registry/storage semantics, not the full networking backend.

## Recovery of development state

The previous `/tmp` worktree and uncommitted W2B files were absent when work resumed. Committed work through 678e055 and the coordinator survived. W2B was reconstructed from the recorded design and rechecked in a persistent repository-local worktree. Previous uncommitted test logs are unavailable and do not count as evidence for this reconstruction. Local Windows execution stopped; subsequent Windows execution is GitHub Actions only.

## Review ledger

| Area | State | Evidence / remaining checks |
| --- | --- | --- |
| Checked private and external I/O | needs-recheck | Review complete; native ACL/no-repair/sharing tests pending |
| Directory identity and publication retries | needs-recheck | Fixed metadata-only root handle, added leaf-rename regression; retry source/target checks as well as MoveFileEx |
| Backup and transfer | needs-recheck | Unix suites pass; native full suites pending, including stale previews and ambiguous pending images |
| Registry and log callers | needs-recheck | Unix suites pass; native full logs and selected registry tests pending |
| Cleanup and archive proof | needs-recheck | Native security checks replace POSIX modes; retained lease regression pending |
| Unix compatibility | needs-recheck | Seven affected package suites pass; full suite/vet pending |
| Final task result | pending | Both native CPUs, affected race checks and final review required before done |
