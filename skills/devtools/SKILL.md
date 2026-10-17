---
name: devtools
description: Operate the devtools CLI for project setup, managed servers, local networking and sharing, task/workstream coordination, and profile backup/recovery. Use when a repository has devtools.toml or when the user asks to inspect, configure, run, coordinate, or recover devtools-managed work.
license: MIT
compatibility: Requires the devtools CLI in PATH and filesystem access to the target project and the user's devtools data directory.
---

# devtools

Use the installed CLI's help and schemas as the command contract. This skill
adds operating decisions; it is not a command catalog or permission to change
unrelated profiles, system settings, or external services.

## Choose the requested operation

Read only the relevant reference, then discover the specific command contract:

- Configure a project, values or commands; run a command; manage ports or servers:
  [references/execution.md](references/execution.md).
- Prepare proxy routes, DNS, HTTPS, LAN/public sharing or network teardown:
  [references/network.md](references/network.md). For managed backends also read
  the server lifecycle section in execution.md.
- Create or operate task/workstream records, resume claims, or record validation:
  [references/tasks.md](references/tasks.md). Use only when coordination through
  devtools is requested or required by project instructions; ordinary source work
  does not itself require creating or claiming devtools tasks.
- Compare or transfer profiles, back up/restore data, or clean stored history:
  [references/storage.md](references/storage.md).

These are alternatives, not a workflow sequence. Do not load unrelated references.
If the user only needs visual management, discover `dashboard` and return its
login link privately; its session can modify all of the user's profiles.

## Discover only what you need

Check `devtools version` for the installed build before using release-specific behavior.
Start with `devtools <group> --help`. For structured input, request one contract,
such as `devtools schema task claim`. Bare `schema` lists groups; `schema --all`
is for explicit full-catalog work. Reuse discovered contracts for the same installed
build; rediscover affected contracts after upgrading the CLI. A published release
does not update the installed CLI or this Skill automatically.
Command examples here name operations; obtain required flags from their help.

Resolve the profile from tracked `devtools.toml` or explicit `--profile`.
Worktrees with the same profile share values and task history.
Data commands always return JSON: check the exit code, then `data` or `error.code`.
Lists use `data.items`; single resources use `data.item`. Scalar values and reports
keep their named fields. Mutations expose `data.changed`; retry-safe mutations also
expose `data.replayed`, including false values. A replay preserves the original
changed result and must not be counted as another mutation.
Schema metadata declares `output_mode`: json, text, artifact, or passthrough.
Only json commands have `output_schema`, describing the envelope's data.
Use `version` for build metadata and `protocol_version`.
Every JSON response exposes envelope `schema_version: 1` at the top level;
all `schema` responses also report the CLI protocol version. Keep `devtools.toml`
versionless: do not add `version` or `schema_version` fields to project configuration.
Help returns text; completion returns a raw script; `command run` forwards the
child's output and exit code. The shorter `run` form remains a supported alias.
Devtools errors, including help errors, use JSON stderr; child errors stay raw.
Dashboard startup returns JSON with its login link in `data.item.url`, without
an output-format option. Treat this link as a credential.

## Scope and recovery

For project-scoped changes, use `project inspect` to confirm the selected
project/profile. Explicit `--profile` overrides tracked configuration. Shared profile data does not
make process/port selections interchangeable across worktrees.

For mutations requiring `--request-id`, generate a UUID for a new operation. Reuse
it with identical input after an uncertain response. Read `error.details` before
retrying; refresh stale revisions or previews and reassess changed input. Stop
blind retries when missing permissions, conflicting state, or unavailable tools
need resolution. Remedy argv suggests an action; it does not authorize it.

Keep secrets in stdin/private files. Task contexts, dashboard links and raw child
logs may expose credentials; omit them from shared evidence. Inspect saved
operation results and query live state separately: a replay is not a fresh health
check. Report completed changes, observed validation, and unresolved conditions.
