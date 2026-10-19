# A12 native bundle exchange verification

The native four-producer/twelve-consumer profile exchange previously verified
fresh local vault/profile/generation and preserved logical slot versions, but
created no nonempty bundle. It now creates frontend (MESSAGE), backend
(MESSAGE/TOKEN) and deploy (TOKEN) once, using revision-guarded CLI edits.
Producer metadata records exact bundle IDs/members and effective local-env
selected slot refs. Each consumer compares all three bundle lists and selections,
alongside the original full-profile/common/local identity and replay checks.

Only synthetic metadata and ciphertext enter artifacts. Secret input remains
stdin and the actual child checks it without revealing it. Restored bundle IDs
and ValueIDs must stay stable while local profile/generation/vault identities
must change. Env overrides are resolved after bundle selection. The fixture does
not grant source sessions, claims or execution handles destination authority.

Reviewed actual bundle/list/inspect CLI schemas, producer sequencing after sealed
secret creation, slot/version expectations, Windows archive ACL, path/Unicode
literals, artifact source/hash checks and finally-owned session cleanup. A local
all-twelve-direction copy exercise is fixture validation only: all binaries in
that exercise are Linux, so it is not native cross-platform acceptance.
Fresh native producer/consumer results are required for A12 and shared B03
acceptance. This adds coverage, not a new product feature or dependency.
