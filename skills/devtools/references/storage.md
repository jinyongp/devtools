# Profiles, transfer, backup and cleanup

## Choose the operation

| Need | Use |
| --- | --- |
| Inspect profile metadata | `profile list/inspect/diff` |
| Copy a profile across devices | `profile export/import` |
| Back up or recover stored profiles | `backup` |
| Archive eligible history | `cleanup preview/apply` |
| Stop servers or remove port/routes | `process/project`, `port`, `proxy`; not cleanup |

- Profile diff reports key/scope/task/instance differences, not values or secret equality. Equal metadata does not prove equal values.
- Local protection: [protection.md](protection.md). Archive passphrases and local vault passphrases are independent.

## Transfer source first

1. Export on the source; no destination preparation is needed for passphrase export.
2. Copy only the encrypted `.age` archive.
3. Initialize/unlock destination protection when needed; preview import.
4. Review metadata diff; apply the same file/source/target with digest and request UUID.

- Protected source export needs its existing unlocked session. Interactive export prompts twice; import once; terminal echo stays disabled.
- Noninteractive: exactly one of `--passphrase-file` / `--passphrase-stdin`. No passphrase in argv/env or persisted by devtools.
- Export defaults to current profile and `./<profile>.age`. An output directory gets that filename. Existing final output fails before snapshot creation; no overwrite.
- Recipient mode is an advanced alternative: destination `profile transfer prepare`, source export with `--recipient` / `--recipient-file`, import with prepared identity or `--identity-file`. Recipient/passphrase inputs are mutually exclusive.
- Preview returns `digest`, `target_exists` and metadata-only `diff`. Single source is inferred; name stays unless `--as` is supplied.
- Apply needs `--apply DIGEST --request-id UUID`; existing target needs `--replace`.
- Replacement creates a private safety archive using the import's passphrase or recipient. No backup configuration is needed for profile import.
- Stale target: new preview/new UUID. Response loss: identical input/original UUID. Active claims, execution credentials, port assignments and live processes do not transfer as active state.

## Backup and restore

- `backup status`: inspect configuration. `backup keygen`: create identity/public recipient without overwrite; keep private identity outside projects.
- `backup configure`: output directory and public recipient. `backup create` without `--profile` includes all stored profiles; use explicit profile for scoped backup. Existing outputs are never overwritten.
- `backup inspect` decrypts using an identity file and returns metadata, not values.
- Restore selects source with `--profile`; `--as` chooses target, otherwise `<source>-restored`.
- Preview, then apply identical file/source/target with `--apply DIGEST --request-id UUID`.
- Existing-target restore needs `--replace` and configured safety backup recipient/directory. Safety backup failure prevents replacement. Unlike profile import, this configuration is required.
- Refresh stale previews; retry uncertain apply with identical input/UUID.
- Restored tasks retain IDs but release claims; obtain a new claim and refresh cursors. Reconnect project config and prepare instance/ports/processes separately; dashboard sessions are not restored.
- Logical archives exclude device keys, vault sessions and launch handles. Protected import needs initialized/unlocked destination protection.
- Restore creates new profile IDs, storage generations and keysets, preserving logical ValueIDs, slot versions and bundle IDs/membership. Identical restored values can still stale a preview.

## Archive and recover history

- `cleanup preview`: `data.id` is plan ID; `data.items` holds candidate IDs. Apply only selected items with `cleanup apply PLAN_ID --item ID --request-id UUID`.
- Preview expires after ten minutes before first apply. Once apply starts, identical retries resume its frozen selection even after TTL; successful siblings may already be archived.
- Partial failure: inspect error details and archives; do not assume rollback.
- Cleanup archives, not purges. Inspect `cleanup archives`; recover with `cleanup restore ARCHIVE_ID`. Changed original location causes conflict.
- `cleanup purge ARCHIVE_ID` permanently removes eligible payload after retention; requires explicit deletion scope.
- Protocol 16 `cleanup preview` adds `retention.verification` and, only after a complete check, buckets with kind/lifecycle/scope, files/logical_items/bytes, verification and cleanup eligibility/reason. Root query caches remain separate from profile filters. A pending check has no partial totals. Protected metadata is authenticated; public/legacy metadata retains existing verification semantics. Payload `record_verified` measures private ciphertext bytes through its authenticated record without decrypting the payload. No inventory starts/unlocks a session, extends idle time, deletes receipts or sweeps expired payloads.
- Restore/purge use archive IDs, no request UUID, and are repeatable. Port release/prune and proxy teardown remain separate operations.
