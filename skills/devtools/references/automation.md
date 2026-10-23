# Guarded writes and approved local runners

Use only for requested automation. Ordinary app execution uses [execution.md](execution.md).

## Guard value mutations

- Discover `schema values revision` and the specific writer schema.
- Supporting set/unset, env create/remove and import accept `--revision` and `--request-id` only as a pair.
- Read the opaque profile-wide token with `values revision`. Keep the token, UUID and exact input for response-loss retries.
- Identical committed input replays; same ID/different input conflicts. Successful no-ops bind IDs; previews do not.
- Guarded `--create-only` treats empty/expired exact slots as existing; inherited common values are not env slots.
- Guarded import preserves layer comparisons and 500,000-byte/5,000-key limits. Same value bytes across supported input methods replay identically.
- `values request show UUID` reads saved metadata without starting/unlocking a daemon or extending idle; protected roots need an existing warm key.
- Missing receipt is `request_not_found`, not success. Completed new receipts replay for 30 days; expired IDs remain spent. Timestamp-free legacy receipts stay unlimited.
- Root deletion, historical full rollback or transfer to another root ends local deduplication scope.

### Generate a secret

- `secret generate KEY --revision REVISION --request-id UUID` stores 32 random bytes as 43 unpadded base64url characters; output is metadata only.
- Protection must already be initialized/unlocked. Default is exact-slot create; `--replace` permits upsert, preserves live expiry and rejects expired slots.
- No kind changes, custom length/encoding/seed or plaintext output. Retry the original request instead of generating again.

## Operate an existing compatible runner

- Discover `schema values execution prepare`, `schema values runner address` and `schema values runner register` before use.
- Requires protected storage and an existing same-OS-user runner implementing private delivery and redaction. CLI schemas alone do not define the receiver transport.
- This workflow operates that runner; it does not implement a receiver or require cloning devtools. If no compatible runner exists, report the missing integration rather than inventing a plaintext bridge.
- Registrations are root-wide; execution records select a profile. Native Windows and WSL stores/identities are separate.

| Stage | Action and invariant |
| --- | --- |
| Register | Use a stable runner UUID, fresh process-instance UUID, and address metadata. Register the live compatible instance; metadata lookup grants no execution authority. |
| Prepare | Supply `selection.keys`, even when empty. With a bundle, provide the full allowed key ceiling. Freeze bytes for the exact external job ID. |
| Approve | Approve that job/input and registered runner instance explicitly. Profile/env/key selection alone is not approval. |
| Deliver | At most one sensitive send. Lost ACK or sender termination after send intent remains unknown. |
| Reconcile | Query the caller engine by original job ID/reference; report verified started/finished state. Reporting does not start work. |

- Use discovered JSON bodies via `--file`/`--stdin`. Keep lowercase request UUIDs, exact bodies, execution/input IDs and caller engine records.
- Recover a prepare through `values execution status --request-id PREPARE_UUID`; inspect current execution status separately from immutable replay.
- Control bodies use the execution's positive integer `item.revision`, not the profile-wide opaque mutation token. Identical retries keep the old body/revision; new decisions inspect status and get a new UUID.
- Preparation grants no execution permission. Unlock does not renew old input authority or extend deadlines. Do not replace an uncertain attempt just to get success.
- An authenticated private channel carries values/patterns; never ordinary JSON, argv, workspace files or provider masking commands.
- Receiver must protect both streams before ACK and retain old patterns through EOF, even after lock/value edits. OS-user authentication alone does not prove log protection.
- The caller owns authorization, isolation, network, engine deduplication and cancellation. Delivery proves neither application use, readiness nor external authentication.
- A registered successor may reconcile the same job/input, but cannot rebind the original recipient. Finished reports also require exit code.
- Docker/remote CI adapters and other-account delegation are outside this workflow.
- Runtime grants/snapshots/registrations are excluded from portable backups. Completed replay lasts 30 days; expired IDs stay spent.
- Status/diagnostics do not unlock or renew idle; locked snapshot cleanup can wait for authenticated mutation. Never delete unknown records or recreate a missing engine ledger as recovery.
