# W1L — Windows storage locks and inherited launch leases

Date: 2026-10-17. Task: `7b507754-102d-4c13-968b-b899f498e3c5`.
Reviewed code: `1220da98398ec4f2f1140ea06b530b85d02df056`.

## Result and scope

Private storage RW locks use LockFileEx, retain checked parent guards, preserve the maintenance writer-intent turnstile and support cancellation. Only kernel lock contention is busy; permissions, invalid handles and other failures remain errors. Ports and log locking use this boundary. Their higher-level storage migration remains W2S/W2B.

Native launch lease files use share mode zero, outside staging at data/process-leases/UUID.lock. Explicit child inheritance preserves exclusion after the parent reference closes. Adoption verifies file identity, private ACL, containment, empty content, data-read access and actual share exclusion, then clears further inheritance. Stable empty files remain after exit and are never interpreted as liveness by existence. Native launch markers are v2/inherited-handle; Unix v1 semantics remain separate.

Completed staging files are flushed/closed before a bounded MoveFileEx WRITE_THROUGH directory publication without replacement. The lease does not block that rename. This does not promise directory fsync or power-loss durability. Native process creation and actual supervisor integration remain W4P/W4D; the W1L tests exercise real helper-process handle inheritance rather than claiming those later product paths are implemented.

## Actual checks

- [Native Actions 39001106127](https://github.com/jinyongp/devtools/actions/runs/39001106127), exact reviewed commit: both amd64 and arm64 PASS. Per CPU: 29 filesystem/path tests, 3 maintenance lock tests, 8 service lease/log lock tests, 1 port lock test, 11 existing process tests and 4 execution/doctor tests. No skip/failure rows. CLI cross-boundary safeguards also pass.
- Local Windows x64: kernel shared/exclusive and inherited handle tests; maintenance shared readers, writer intent and canceled waiter; ports busy/cancel/reacquire; log readers/writer; v2 launch publication, retained files, duplicate IDs, cleanup, invalid snapshot and actual child adoption PASS. The separate-user filesystem fixture is CI-only; its local skip is not counted as a pass.
- Full Unix `go test ./...` and `go vet ./...` PASS. Focused maintenance/platformfs/ports/services race checks PASS. Windows amd64 tests and ARM64 CLI cross-build PASS.
- Retained JSONL under W1L-actions-39001106127 has normalized text line endings/trailing blanks.

## Last review loop

| Area | State | Findings and recheck |
| --- | --- | --- |
| General lock semantics | reviewed | Real cross-process shared/exclusive behavior, invalid handle error, writer intent and cancellation verified; close retains/releases parent guards |
| Lease identity and lifetime | reviewed | Review rejected shared and metadata-only handles as insufficient ownership. New rejection and actual child-adoption tests pass on both CPUs |
| Staging publication | reviewed | Lease stays outside stage. Review fixed UUID staging suffix to match strict cleanup grammar; no-replace and parent-close inheritance tests pass |
| Failure/recovery boundaries | reviewed | Failed launch removes only unpublished private stages; empty leases remain. Invalid snapshot, duplicates and cleanup fixtures pass; no directory-sync no-op introduced |
| Callers | reviewed | Maintenance, ports, log and service lease call sites retain wrappers rather than prematurely closing embedded handles; Unix behavior preserved |
| Platform separation | reviewed | Unix inherited-fd tests and implementation remain under Unix tags; native equivalents test real handles without test skips |
| Scope and evidence | reviewed | Both native architectures and Unix checks pass at the immutable reviewed commit. Later storage/launcher integration must rerun affected checks |

No unchecked required W1L gate or remaining in-scope finding. This result does not pass the higher-level storage, managed launcher, final product support or W9 gates.
