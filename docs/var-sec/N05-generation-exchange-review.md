# N05 cross-device identity acceptance follow-up

The complete 2eb9fb7 native run 39266251666 passed, including all 12 directional
profile exchanges. Artifact/code review showed that those exchanges asserted
restored values/tasks/replay but did not directly compare source and destination
profile/storage identities. The lower-level logical transfer tests covered new
keys/incarnation, but the task also requires an actual cross-device generation
check. The old result is not relabeled as that stronger check.

Producers now preserve safe source vault ID and common/local selected input
identities/slot references in the per-run manifest. Consumers explicitly require
a different target vault ID, profile ID and storage generation, with unchanged
logical value IDs, slot IDs, versions and common/env sources. Real restored-child
verification remains, with expected synthetic secret bytes provided through binary
stdin rather than embedded in a child argument. No source key, admission ticket,
session credential, ciphertext secret or plaintext fingerprint enters the manifest.

The local producer and a local-copy consumer integration passed with the exact
2eb9fb7 Linux image. The local-copy label is explicitly a same-platform simulation,
not macOS/Windows proof. Results are
`/tmp/devtools-values-plan/N05-generation-consumer-local-binary.json`; both fresh
identities and preserved slots are checked. Input bytes preserve newline/UTF-8
semantics across Windows and Unix. Only synthetic disposable roots are created,
and all owned vault workers are retired before cleanup.

Reviewed producer/consumer manifest agreement, replay ordering, sealed-profile
verification, source/target identity comparison, preserved logical identities,
per-run commit/binary/archive hashes, stdin/output safety and cleanup. No eligible
finding remains in this verifier unit. The fresh four-producer/12-consumer native
run remains required before N05 completion. A12/Z01 are separate required gates.
