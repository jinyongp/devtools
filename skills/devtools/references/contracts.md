# Installed CLI contracts

## Discover narrowly

- Bootstrap with `schema version` and `DEVTOOLS_FORMAT=json` in the child environment, even if the parent preference is text/invalid. Inspect response `global_options`: a format entry with root-prefix placement and json in values authorizes root `--format json` on later structured calls. Without it, retain old argv. Stop on bootstrap failure before starting an operation; never detect support with mutation retry. Do not change the parent environment.
- `version` identifies build metadata and `protocol_version`.
- `schema <command>` describes that operation; bare `schema` lists groups. Use `schema --all` only for full-catalog work.
- `config schema` describes TOML/edit/diagnostic structures without project lookup.
- CLI and Skill update separately. Rediscover affected contracts after a CLI upgrade.

## Parse by output mode

| Mode | Handling |
| --- | --- |
| `json` | Structured output; request root `--format json` on supporting builds, then check exit/ok and `data` or `error.code/details` |
| `text` | Help/plain text; do not parse as an envelope |
| `artifact` | Follow the command's artifact contract |
| `passthrough` | Child output and exit code; no JSON wrapping |

- Read `output_mode` with `presentation`: normal structured commands support text/json, machine discovery supports JSON, native stdout has formats=[]. Only structured json commands have `output_schema`, describing JSON envelope `data`. Normal schema inputs allow format auto/json; schema help remains native text.
- Root `--format` overrides DEVTOOLS_FORMAT, including explicit auto. Both omitted use auto: known agent signals or non-TTY stdout select JSON, otherwise text. stdout's decision also governs devtools errors when stderr is a terminal. Child output protection `--output` is separate.
- JSON envelopes use top-level `schema_version: 1`. Schema responses also report CLI protocol version. Neither belongs in project TOML.
- Lists: `data.items`. Single resources: `data.item`. Scalars/reports keep named fields.
- Mutations expose `data.changed`; retry-safe ones also expose `data.replayed`, including false.
- Protocol 16 adds `cleanup preview.retention`; rediscover its schema. `verification_pending` omits partial buckets. A verified bucket distinguishes physical files/bytes from logical requests and states why cleanup is eligible or unavailable. Inventory does not authorize deletion.
- Protocol 17 preserves typed input-correction facts and JSON field paths in diagnostics. Operation indexes include zero; `observed_exact: false` marks a bounded-read observation rather than the full input size. `rule_id` and `options` identify a static option rule without returning input values. Rediscover the schema after an upgrade; request fingerprints and storage formats are unchanged.
- Replays preserve the original changed result; do not count another mutation or infer current health.
- On supporting builds `vault rewrap` returns `item`, `request_id`, `changed`, `replayed`. Optional vault status `item.rewrap` is unverified routing metadata; pending recovery uses `details.target.request_id`, `retry:same_request` and scoped remedies.
- Devtools errors use the selected format; schema and malformed format inputs use JSON. Existing prompts/child streams can precede the diagnostic. Completion/help/artifact/child/private stdout retains its own wire even with --format json.
- Dashboard startup is structured output; explicitly request JSON to read the credential-bearing URL at `data.item.url`. Return it only to its user.
- Text is a task-focused summary, not a complete diagnostic dump or stable parser interface. Typos show correction candidates; routine parser/effect/retry metadata stays in JSON. Partial/uncertain effects and same-request recovery remain explicit in readable language. Complete Next actions retain public argv as whole copyable shell lines; required inputs remain templates. Do not re-run a mutation to obtain another presentation.

## Recover the same operation

- New operation: new UUID. Uncertain response: preserve UUID and exact input.
- Read `error.details` before retrying. Stale revision/preview: refresh and reassess before a new decision.
- A saved operation result and live state answer different questions. Inspect both when execution outcome is uncertain.
- Remedies contain suggested argv/required inputs; missing inputs or permissions must be resolved, not bypassed.
