# Managed storage upgrade inventory

Status: implementation in progress. This is the T09 inventory for workstream
`76536172-4285-4efa-9ad6-528d3fa1de5f`. It describes managed local storage, not
project TOML syntax, live service markers, or every historical CLI alias.

## Formats and ownership

| Domain | Recognized source | Current target and converter |
| --- | --- | --- |
| Profile filenames | hex-encoded profile filename containing actual state | `p1-<sha256>.json` plus private `.identity` document; reuse `profilekey.Replacement` |
| Public values | `values.State`, `version=1` | `vault.Profile`, `vault_format=1`; reuse `convertLegacy`, verify logical layers before publication |
| Secret values | plaintext legacy `values.State` | sealed current profile and authenticated catalog through existing `values.MigrateLegacy`/`vault.ForwardStore`; existing protection and an authenticated key are prerequisites |
| Current values | public or sealed `vault.Profile` | unchanged bytes/identity/revision unless a filename or receipt needs migration |
| Task journal | physical JSON journal with logical version 1 or 2 | task container 3: head, generation, WAL, snapshot, receipt/context coordination files; reuse `migrateToV3` without a dummy task mutation |
| Task snapshot | container 3 snapshots with format version 3 or compact format 4 | both remain valid current storage; current historical events and reducers are not removed as disk-format fallback |
| Value edit receipts | hex profile filename or pre-s2 canonical filename containing exact request UUID text | s2 name derived from the exact UUID text, using current receipt protection and scope; preserve fingerprint and saved outcome |
| Backup receipts/preview key | legacy plaintext protected-root material | existing legacy vault migration seals the receipt and verification material; do not recreate discarded preview capabilities |
| Portable archives | supported logical values/task/profile backup payloads | normalize through the same domain conversion boundary during import, never publish logical archives as plaintext disk profiles |

The current canonical profile layout includes a small downgrade-fence marker at an
address readable by older writers. It contains no legacy value/task payload. Keep
this fence while those writers can address that path; removing live legacy data
does not authorize removing the concurrency and downgrade defense.

Primary implementation boundaries: `internal/profilekey/{profilekey,snapshot}.go`,
`internal/values/{modern,modern_manage,receipt,vault_migration,backup}.go`,
`internal/tasks/{store,storage_loader_v3,storage_migrate_v3,storage_v3}.go`.
Vault initialization/recovery and backup/profile import call the converters too.
The coordinator must not depend on the task registry it is upgrading.

## Inspect, plan, apply, verify

The coordinator inventories every managed profile/domain in the selected data
root. Inspection never creates locks, initializes a vault, opens secret slots,
renews an idle session, recovers transactions, or requires project configuration.
Unknown/future versions and damaged files remain explicit errors, not empty state.
Inspection may report that authentication or ordinary maintenance recovery is
needed without exposing values or treating a cold session as a corrupt profile.

Domain plans include source/target and a fingerprint, not plaintext payloads.
Acquire any existing key/session outside storage locks. Revalidate the fingerprint
inside the existing launch/maintenance/profile lock order before publishing.
Keep old writer lock objects and intent ordering. A new lock name alone cannot
exclude a running older binary. Busy writers, read denial, and missing lock
preparation have different states and remedies.

Apply reuses existing private transaction/forward-store publication. Convert and
verify the logical payload before its single commit, preserve a private recovery
copy or the existing recoverable transaction material, then verify the published
current representation. Only then remove the old live payload. A crash or I/O
failure must leave the old committed state or a resumable transaction; unrelated
domain success never turns a partially migrated root into Ready.

Current stores are no-ops. Repeated migration preserves existing profile/value/
slot identities, revisions, receipt replay, task history and execution contexts.
Legacy records that did not have current identities receive them once at commit.
Do not use wall-clock time, repeated query, or a dummy task event to manufacture a
new identity or a new logical task revision during migration.

## User entry points and update

The planned `doctor --storage` performs passive root inspection. The planned
`doctor --storage --fix` uses the same coordinator to prepare/recover/migrate and
reinspect. Ordinary doctor, `--check`, and normal reader/writer paths do not run
schema migration; they report `migration_required` with the same-root remedy.

Owned Unix/Windows updates invoke the installed new executable's coordinator,
including a same-version update with pending work. Keep executable replacement and
data migration outcomes separate. A locked vault, needed protection choice, busy
writer, readonly policy or unsupported format cannot be silently reported as full
success. Homebrew retains its package ownership and points to the same doctor.
No installer chooses a new vault backend, passes keys in argv/environment, kills
old writers, or scans arbitrary external roots, archives and project files.

Executable rollback before data commit retains the existing installer behavior.
An incompatible data commit must not be followed by automatic binary downgrade.
Recovery uses retained transaction material or a compatible backup restored into a
fresh root; changing the executable alone is not data rollback.

## Validation and limits

Fixtures cover each source row, current no-op, mixed profiles/domains, skipped
versions, future format, private-file attacks, cold/locked protection, busy old
writers, fingerprint changes, and stage/commit/verify/cleanup interruption.
Compare independent logical expectations and exact request replay after import or
migration. Current public/secret metadata authentication promises remain distinct.
Platform evidence comes from the existing Unix and Windows workflows at the
tested main SHA; a cross compile is not native execution evidence.

The implementation request authorizes code, isolated fixtures, main commits and
existing workflow validation. It does not run migration against the user's live
value store, replace their installed CLI, publish a release, or rewrite Git history.
