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

## Storage formats and updates

On protocol 23, `doctor --storage` inspects the selected data root without writes;
`doctor --storage --fix` prepares, recovers, converts known formats and rechecks them.
Ordinary reads and writes report `migration_required` instead of converting old
profiles, physical task journals or receipts. Current logical task history remains readable.
Protected conversion borrows only an existing unlocked key, preserving its original
idle deadline. A locked/missing vault requires an explicit protection action; doctor
never chooses a backend or prompts for an unlock. Follow the reported prerequisites.

Protocol 25 long CLI operations report their current stage, lock wait and
elapsed time on text stderr. ANSI terminals update one line; plain text streams
use bounded line updates. Prompts and foreground output suspend the shared UI.
Updater waits also show elapsed time while the installer's JSON stays private.
JSON and detected agents receive no progress output.
The complete storage diagnosis, including lock waits, obeys `--timeout` (default 30s) and
cancellation. Retain the partial report/effects and resume after resolving the
reported prerequisite; progress is not a commit or cleanup guarantee.

Owned installer/update paths invoke this coordinator in the new executable, also
on a same-image retry. Check both `binary_changed` and `storage.ready/status`.
An incomplete storage outcome exits 3 and retains the installed binary and recovery
state; resolve the cause and run `doctor --storage --fix`. Homebrew updates retain
package ownership and provide that next step. Do not downgrade a binary as data rollback.

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
- Apply needs `--apply DIGEST`; existing target needs `--replace`. Protocol 23 generates an omitted request ID; older schemas may require it explicitly.
- Replacement creates a private safety archive using the import's passphrase or recipient. No backup configuration is needed for profile import.
- Stale target: new preview/new UUID. Response loss: identical input/original UUID. Active claims, execution credentials, port assignments and live processes do not transfer as active state.

## Backup and restore

- `backup status`: inspect configuration. `backup keygen`: create identity/public recipient without overwrite; keep private identity outside projects.
- `backup configure`: output directory and public recipient. `backup create` without `--profile` includes all stored profiles; use explicit profile for scoped backup. Existing outputs are never overwritten.
- `backup inspect` decrypts using an identity file and returns metadata, not values.
- Restore selects source with `--profile`; `--as` chooses target, otherwise `<source>-restored`.
- Preview, then apply identical file/source/target with `--apply DIGEST`. Keep the generated request ID; use it explicitly for an identical retry.
- Existing-target restore needs `--replace` and configured safety backup recipient/directory. Safety backup failure prevents replacement. Unlike profile import, this configuration is required.
- Refresh stale previews; retry uncertain apply with identical input/UUID.
- Restored tasks retain IDs but release claims; obtain a new claim and refresh cursors. Reconnect project config and prepare instance/ports/processes separately; dashboard sessions are not restored.
- Logical archives exclude device keys, vault sessions and launch handles. Protected import needs initialized/unlocked destination protection.
- Restore creates new profile IDs, storage generations and keysets, preserving logical ValueIDs, slot versions and bundle IDs/membership. Identical restored values can still stale a preview.

## Archive and recover history

- `cleanup preview`: `data.id` is plan ID; `data.items` holds candidate IDs. Apply only selected items with `cleanup apply PLAN_ID --item ID`; keep the returned request ID for recovery.
- Preview expires after ten minutes before first apply. Once apply starts, identical retries resume its frozen selection even after TTL; successful siblings may already be archived.
- Partial failure: inspect error details and archives; do not assume rollback.
- Cleanup archives, not purges. Inspect `cleanup archives`; recover with `cleanup restore ARCHIVE_ID`. Changed original location causes conflict.
- `cleanup purge ARCHIVE_ID` permanently removes eligible payload after retention; requires explicit deletion scope.
- `cleanup preview` reports retention buckets with verification and eligibility/reason. Protocol 23 can include public buckets while protected verification is pending; do not treat them as authenticated protected totals. Payload bytes are measured without decrypting them. Inventory never unlocks/renews a session or deletes data.
- `cleanup maintain --dry-run` inspects the next bounded owned pass; omit dry-run only for authorized maintenance. Resolved details expire at 30 days, captures at 7 days, query snapshots at 30 minutes. Related writes resume automatic maintenance. Live/unknown/recovery references, exact replay/restore bodies and user tasks retain their own lifetime. See [navigation.md](navigation.md).
- Restore/purge use archive IDs, no request UUID, and are repeatable. Port release/prune and proxy teardown remain separate operations.
