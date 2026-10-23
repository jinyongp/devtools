# Values, environments and reusable selections

## Choose the capability

| Need | Use |
| --- | --- |
| Public config value | `var` / `variable` |
| Credential | `secret` / `sec` |
| Environment override | `env` and `--env` |
| Repeated subset of var/sec keys | `bundle` |
| Inspect selected inputs or changed deliveries | `values inspect/uses` |
| Port or local URL | `bind`; see [ports.md](ports.md) |

- Keep declarations tracked; keep stored secrets outside project files.
- Prefer configured injection/bundles to scripts that read secrets and rebuild environments.
- Protection setup/unlock: [protection.md](protection.md). Guarded writes/generation: [automation.md](automation.md).

## Store and override

- Create an env before selecting it. Without `--env`, operations address common scope.
- With `--env`, reads merge common and overrides; writes/unsets affect only that env. Removing an override reveals common.
- A key's var/sec kind is profile-wide and survives unsetting. Remove directly stored env values before removing an env.
- Discover `schema var set`: newer builds accept exactly one of `--value`, `--file`, `--stdin`; older builds require `--value`.
- Secret set accepts exactly one of `--file` or `--stdin`, never `--value`.
- File/stdin stores one raw UTF-8 value, preserving whitespace/newlines; empty input stores empty. Stdin must be redirected. This is not dotenv parsing.
- Dotenv import: preview with `--dry-run`; existing kinds remain; new `--var` keys are public, other new keys secret. Changed stored values need explicit `--overwrite`. Failed import preserves all values; source files remain.

## Execution selection

| Execution | Profile value injection |
| --- | --- |
| Named command, default | Off; parent environment inherited |
| Named command, `inject = true` | Selected common/env values overlay parent |
| Explicit CLI `--env` | Enables injection; overrides configured env |
| Direct `run -- PROGRAM` | Injects selected profile values |

- `bind` is explicit per-command injection, independent of `inject`.
- Bundle precedence: CLI `--bundle` → command `bundle` → project `bundle` → `all`.
- `--bundle` alone does not enable named-command injection.
- `none` selects no profile values; `--env` does not undo it. It does not remove inherited credentials.
- A bundle shares var/sec ValueIDs; it neither copies values nor chooses an env. Register membership once, then reuse its name.
- Discover `schema bundle set`; use `bundle list` revision and a request UUID for edits. `set` replaces the full membership atomically.
- `import --bundle NAME` adds imported keys in the same transaction, preserving import kind/overwrite rules.
- Unknown bundles and out-of-selection `${var.KEY}` template references fail; do not fall back to `all`. Port/proxy bindings are not bundle members.
- Rename preserves the bundle ID; removal/membership changes do not delete values. Changes affect later executions. Bundles are not an app sandbox.

## Inspect actual use

- `command inspect`: configured selection. Execution ID: selection actually used by that run.
- `values inspect`: selected slot/version/incarnation metadata. `values uses`: declarations versus recorded deliveries.
- Neither report proves application reads or external authentication. Passive inspection does not unlock or extend a key session.
- Locked verification may be `verification_pending`; old executions without delivery metadata are unknown. Public-only bodies retain `integrity_verified: false` even in a protected root.
- Changed effective input becomes pending; default policy is notification. Unselected changes, hidden common overrides and same-value writes do not change delivery.
- Existing processes retain their environment and output patterns until exit. Use an authorized managed restart to apply changed input.

## Optional automatic restart

- For a selected managed server, `values_update = "restart"` applies effective input changes automatically on supporting builds.
- Do not enable it for migrations, deploy jobs or foreground work. Ended one-shot/foreground commands are never repeated automatically.
- Inspect `value_watch` for input and `value_apply` for requested/applied/failed/paused plus replacement `next_id`.
- Preparation failure keeps the old app; failure after stop can leave an outage. Stored values are not rolled back; siblings are not stopped.
- Failed/unknown attempts are not automatically resubmitted. Three replacements in 30 seconds also pause the chain.
- Inspect the current project/execution and resolve the cause. An authorized manual restart with a new UUID starts a new attempt; never infer success from a PID or delete coordination files.
