# Installed CLI contracts

## Discover narrowly

- `version` identifies build metadata and `protocol_version`.
- `schema <command>` describes that operation; bare `schema` lists groups. Use `schema --all` only for full-catalog work.
- `config schema` describes TOML/edit/diagnostic structures without project lookup.
- CLI and Skill update separately. Rediscover affected contracts after a CLI upgrade.

## Parse by output mode

| Mode | Handling |
| --- | --- |
| `json` | Check exit code and `ok`; inspect `data` or `error.code/details` |
| `text` | Help/plain text; do not parse as an envelope |
| `artifact` | Follow the command's artifact contract |
| `passthrough` | Child output and exit code; no JSON wrapping |

- Schema metadata declares `output_mode`; only JSON commands have `output_schema`, describing envelope `data`.
- JSON envelopes use top-level `schema_version: 1`. Schema responses also report CLI protocol version. Neither belongs in project TOML.
- Lists: `data.items`. Single resources: `data.item`. Scalars/reports keep named fields.
- Mutations expose `data.changed`; retry-safe ones also expose `data.replayed`, including false.
- Replays preserve the original changed result; do not count another mutation or infer current health.
- On supporting builds `vault rewrap` returns `item`, `request_id`, `changed`, `replayed`. Optional vault status `item.rewrap` is unverified routing metadata; pending recovery uses `details.target.request_id`, `retry:same_request` and scoped remedies.
- Devtools errors, including help errors, use JSON stderr. Child streams follow output-protection policy; completion returns a raw script.
- Dashboard startup returns JSON, with the credential-bearing URL in `data.item.url`; no output-format option is needed.

## Recover the same operation

- New operation: new UUID. Uncertain response: preserve UUID and exact input.
- Read `error.details` before retrying. Stale revision/preview: refresh and reassess before a new decision.
- A saved operation result and live state answer different questions. Inspect both when execution outcome is uncertain.
- Remedies contain suggested argv/required inputs; missing inputs or permissions must be resolved, not bypassed.
