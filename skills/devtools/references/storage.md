# Profiles, backups and cleanup

## Inspect stored profiles

Use `profile list`, `profile inspect NAME`, and `profile diff LEFT RIGHT` for metadata-only
profile management. Diff reports key/scope and task/instance differences, not stored
values or secret equality; no metadata difference does not prove equal values.

## Transfer a profile

Use `profile export` and `profile import` for the default cross-device transfer.
The default is source-first: run export on the source without preparing the destination,
copy the resulting `.age` file, then import it on the destination. Interactive export
prompts twice for a passphrase with terminal echo disabled; interactive import prompts once.
Devtools does not persist the passphrase. For non-interactive use, select exactly one of
`--passphrase-file` or `--passphrase-stdin`; never place a passphrase in argv or env.

Export defaults to the current project's profile and to `./<profile>.age`. If `--output`
points to an existing directory, export writes `<profile>.age` inside it. Existing final
outputs fail before snapshot construction and are never overwritten. Copy only the resulting
encrypted archive from source to destination.

Recipient-based transfer remains an advanced compatibility path. Run
`profile transfer prepare` on the destination, then use `--recipient` or
`--recipient-file` on export. Recipient archives can use the prepared local identity or an
explicit `--identity-file` on import. Recipient and passphrase inputs are mutually exclusive.

Import without `--apply` previews and returns `digest`, `target_exists`, and metadata-only
`diff`. It infers a single source profile and keeps its name unless `--as` is supplied.
Review that preview, then apply the same file/source/target with
`--apply DIGEST --request-id UUID`. Existing targets require `--replace` at apply.
Before replacement, devtools creates a private safety archive using the same encryption mode
as the active import: the same passphrase for passphrase archives, or the same recipient for
recipient archives. No backup configuration is required. Stale targets require a fresh
preview and a new request ID. Identical apply retries replay the stored result; changed inputs
conflict. Task claims, execution credentials, port assignments, and live processes are not
transferred as active state.

## Encrypted backup and restore

Use `backup status` to inspect configuration. `backup keygen` creates a private
identity and public recipient file without overwriting existing files. Keep the
identity outside project files; `backup configure` stores the default output
directory and public recipient. `backup create` without --profile includes all
stored profiles; use an explicit profile for a scoped backup. Existing outputs
are never overwritten. `backup inspect` decrypts with an identity file and returns
metadata, not plaintext values.

`backup restore` differs from profile import: select the source with --profile;
--as chooses the target, otherwise it defaults to <source>-restored. Preview first,
then use the same file/source/target with --apply DIGEST --request-id UUID.
Replacing an existing target requires --replace and a configured safety backup
recipient/directory; failure to create that backup prevents replacement. This
configuration requirement does not apply to profile import's same-mode safety
archive. Refresh a stale preview; reuse identical apply input/UUID after an
uncertain response.

Restored task history keeps IDs but releases active claims; obtain a new claim.
Config files, instance/port assignments, live processes and dashboard sessions
are not restored. Reconnect the target project and prepare its local execution
state separately. Refresh task cursors after restore.

## Cleanup and archives

`cleanup preview` returns data.id as PLAN_ID and data.items candidate IDs. Apply
only selected candidates with `cleanup apply PLAN_ID --item ID --request-id UUID`.
A preview lasts ten minutes; refresh it if stale before the first apply. Once
apply starts, identical retries resume its fixed selection, including after TTL
expiry; successful siblings may already be archived. Inspect error details and
archives after partial failure rather than assuming rollback.

Cleanup archives eligible data; it does not purge the archive. Use `cleanup
archives` then `cleanup restore ARCHIVE_ID` for recovery. Restore conflicts if the
original location has changed. `cleanup purge ARCHIVE_ID` permanently removes an
eligible payload after the retention period and needs explicit deletion scope.
Restore/purge use archive IDs and do not take request UUIDs. They are repeatable.
Port release/prune and proxy teardown are separate operations; do not substitute
cleanup for stopping running servers or deactivating routes.
