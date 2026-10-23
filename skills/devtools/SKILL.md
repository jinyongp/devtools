---
name: devtools
description: Configure and run devtools-managed projects using existing commands, dynamic ports, environment values, servers, and local URLs. Use alongside implementation or debugging when devtools.toml exists and work involves setup, execution, port conflicts, or service connections, even when the request does not name devtools. Also use for requested devtools networking, task coordination, and profile recovery. Unrelated source edits do not require devtools operations.
license: MIT
compatibility: Requires the devtools CLI in PATH and filesystem access to the target project and the user's devtools data directory.
---

# devtools

## Start with existing capabilities

1. Inspect project instructions and existing `devtools.toml` before changing setup or launch code.
2. Match the requested behavior to the table below. Read only the relevant reference.
3. Check `devtools version`, then the selected group's `--help` and specific command schema.
4. Reuse an existing command or change its declarations. Add wrapper code only for a confirmed capability gap or an explicit project requirement.
5. For changes, validate configuration and affected behavior. For inspection requests, keep operations read-only. Report what actually passed.

Use this skill alongside the source-work workflow. Do not create task records merely because a project uses devtools.

## Choose by requested behavior

| Request | Existing capability | Read |
| --- | --- | --- |
| Set up tools; add/change a launch or check command | `config`, `command`, `doctor` | [execution.md](references/execution.md) |
| Run once; keep a server alive; restart; wait for readiness | `command run`, `process`, `project` | [execution.md](references/execution.md) |
| Pick a free port; avoid worktree collisions; pass ports or URLs to apps | `[ports.NAME]`, `port`, `serve`, `bind`, `${bind.NAME}` | [ports.md](references/ports.md) |
| Inject env values or secrets; reuse a subset; inspect changed input | `var`, `secret`, `env`, `bundle`, `values` | [values.md](references/values.md) |
| Initialize/unlock/change local protection; fix protected execution | `vault` | [protection.md](references/protection.md) |
| Stable local domains; HTTPS; LAN/public sharing | `proxy`, `expose` | [network.md](references/network.md) |
| Claim/resume requested tasks; edit plans; record validation | `task`, workstreams | [tasks.md](references/tasks.md) |
| Transfer profiles; back up, restore, or clean history | `profile`, `backup`, `cleanup` | [storage.md](references/storage.md) |
| Guard value mutations; deliver frozen values to an existing local runner | `values revision`, `values execution`, `values runner` | [automation.md](references/automation.md) |

- Windows/WSL execution, installation, shell or host changes: also read [platforms.md](references/platforms.md).
- JSON bodies, output parsing, replay or protocol compatibility: read [contracts.md](references/contracts.md).
- Visual management only: discover `dashboard`; return its login link privately. The session can modify all profiles.

## Discover the installed contract

- Commands: `devtools <group> --help`, then `devtools schema <group> <command>`.
- TOML fields and binding forms: `devtools config schema`. Command schemas and configuration schemas serve different purposes.
- Existing setup: `project inspect`, `command list`, `command inspect NAME`; discover support before using newer `config inspect` or `command explain`.
- Missing support: inspect the error and installed version. Do not assume the current source or a published release matches the installed CLI.
- Missing CLI: verify the project's execution environment before diagnosing absence. Resolve authorized setup or report the gap; do not replace devtools with a wrapper merely because it was unavailable in one shell.
- Reuse contracts for the same build. Avoid `schema --all` unless reviewing the full catalog.
- On protocol 13 and later, syntax examples may contain placeholders. Follow `workflow` for
  real revisions/IDs and new UUID versus identical-input retry. `values execution
  status` accepts one execution ID or `--request-id`, never both.
- Ordinary operation needs the installed CLI and these bundled references, not a separate devtools source checkout.

## Keep scope and state explicit

- Confirm the selected project/profile before project changes. Explicit `--profile` overrides tracked configuration.
- Same-profile worktrees share values and task history; processes and port assignments belong to execution locations.
- Keep `devtools.toml` versionless. Never add top-level `version` or `schema_version`.
- Use a new UUID where `--request-id` is required. After response loss, retry identical input with the original UUID; inspect current state separately.
- Refresh stale revisions/previews before a new decision. Stop blind retries on missing permissions, conflicting ownership or unknown execution outcomes.
- Keep secrets in stdin/private files. Omit task contexts, dashboard links, credentials and unfiltered historical logs from shared evidence.
- Remedies suggest actions; they do not authorize unrelated installation, host changes, external exposure or data replacement.
