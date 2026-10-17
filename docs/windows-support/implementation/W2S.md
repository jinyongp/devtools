# W2S — Native task, values and profile storage recovery

Date: 2026-10-17. Task: `172786e8-9523-48ae-8db6-6e50d2324e76`.
Reviewed code: `f9b4509798fe6fe75f8965726984b06367cb05af`.

## Result and scope

Values, profile identity, task-v3/WAL/pending/commit and their maintenance transaction paths use guarded native private-file opens, explicit user ownership/protected directory ACLs, file flush/close and bounded WRITE_THROUGH publication. General locking remains the W1L backend. Unix permission/no-follow/dir-sync behavior is retained in its backend. There is no Windows directory-sync success stub: complete native transaction operations implement the Windows publication/recovery policy instead.

The stored schemas, profile hash keys, request IDs, replay/changed semantics, revision checks, claim coordination, WAL checksum/torn-tail validation and before-image rollback remain shared. Read/completion paths preserve existing logical storage and missing-domain behavior; completion does not create stores or locks. Ordinary reads retain the existing maintenance/recovery gate and its operational lock metadata.

Native generation publication flushes the WAL before publishing its head/marker transaction. Pending refs use no-replace WRITE_THROUGH moves; committed pointer success requires successful native image publication. A failed publication cannot become success merely because a committed pointer is visible. Recovery retains before-images until the phase allows their cleanup. This is process-interruption recovery, not a promise of directory fsync or power-loss durability.

Higher-level backup, transfer, ports registry and process/log data integration remain W2B. Public CLI commands remain behind the intermediate platform gate until the remaining Windows product paths are ready.

## Actual checks

- [Native Actions 39002271156](https://github.com/jinyongp/devtools/actions/runs/39002271156), exact reviewed commit: amd64 and arm64 PASS. Each CPU has **194 complete storage/recovery test and subtest rows**, no skip/failure. The >128 MiB streaming before-image test is explicitly enabled. Task suite duration: 214.537 s x64 and 56.111 s ARM64.
- Both native CPUs retain passing filesystem/path 29, process 11, execution/doctor 4, console 10, service lease/log lock 8, ports lock 1 and focused maintenance 4 test rows, without skip/failure.
- New task tests use actual TerminateProcess before and after the WAL commit. Retrying the same request recovers pending state, replays only the committed request, and leaves exactly one new task. Actual sharing-denied committed-pointer replacement fails closed; the next Acquire restores the original before-image after the reader closes.
- Local Windows x64: complete tasks/values/profilekey/maintenance suites, native abrupt-crash cases and sharing-denied rollback PASS. The large streaming fixture also PASS with its explicit flag.
- Unix full Go suite/vet and focused task/values/profilekey/maintenance/platformfs race checks PASS. After the elevated recovery fix, the affected Unix suites and vet PASS again. The production correction is Windows-only; Unix publication semantics remain intact. ARM64 CLI cross-build PASS.
- Raw JSONL in W2S-actions-39002271156 has normalized line endings/trailing blanks. Later uncommitted W7P tooling is excluded from this immutable W2S result.

## Last review loop

| Area | State | Findings and verification |
| --- | --- | --- |
| Private I/O and callers | reviewed | Windows uses checked handles/ACLs/parent guards; Unix retains no-follow/nonblocking and permission checks. Cache/query cleanup now checks native private files rather than POSIX mode bits |
| Identity and long profiles | reviewed | Native tests exposed invalid-name instead of ENAMETOOLONG for an overlong legacy component. NTFS addressability classification now preserves canonical long-profile state/receipt replay; existing profiles remain case-sensitive logical identities |
| Task coordination and schemas | reviewed | Complete claim race, revision, replay, edit and validation suites run natively. No journal/WAL/pending/identity schema change |
| WAL and crash boundary | reviewed | Actual abrupt process termination on both sides of WAL commit; pending recovery, checksum/torn-tail/corruption and same-request retry tests pass |
| Publication and multi-file rollback | reviewed | Native no-replace ref moves and successful WT pointer publication are required. Sharing-denied commit retains recoverable before-images; complete recovery/large streaming suites pass |
| Elevated and ordinary tokens | reviewed | CI found os.CreateTemp gives Administrators ownership under an elevated token, breaking private recovery. Production restore temp now uses explicit private creation. Fixtures also request explicit user ownership. Both elevated native runners and local ordinary-token tests pass |
| Unix durability coverage | reviewed | Unix directory-sync fault-injection tests are preserved under Unix tags. Native tests verify actual WT sharing failure and recovery instead of passing a no-op directory-sync function |
| Read and missing state | reviewed | Existing logical no-change, migration replay and missing/completion behavior is retained; maintenance operational lock metadata is distinguished from logical store mutations |
| Scope and evidence | reviewed | All required W2S gates pass at the immutable reviewed commit. Later backup/transfer/log/process/installer integration remains separately required |

No remaining in-scope W2S finding or unchecked required W2S gate. This record does not pass W2B, final product integration or W9.
