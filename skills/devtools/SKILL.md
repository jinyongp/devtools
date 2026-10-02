---
name: devtools
description: Operate the devtools CLI for project setup, managed servers, stable local proxy routes, ports, and task/workstream coordination. Use when a repository has devtools.toml or when the user asks to inspect, configure, run, coordinate, or recover devtools-managed work.
license: MIT
compatibility: Requires the devtools CLI in PATH and filesystem access to the target project and the user's devtools data directory.
---

# devtools

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
Use `version` for build metadata and `protocol_version` (6 for this source).
Every JSON response exposes envelope `schema_version: 1` at the top level;
all `schema` responses also report the CLI protocol version. Keep `devtools.toml`
versionless: do not add `version` or `schema_version` fields to project configuration.
Help returns text; completion returns a raw script; `command run` forwards the
child's output and exit code. The shorter `run` form remains a supported alias.
Devtools errors, including help errors, use JSON stderr; child errors stay raw.
Dashboard startup returns JSON with its login link in `data.item.url`, without
an output-format option. Treat this link as a credential.

## Prepare and run

Use `doctor COMMAND` before a configured command when setup is uncertain.
A successful diagnosis can still have `data.ready: false`; inspect `checks[].remedies`.
Use `diagnostics` when the question spans the user's devtools installation instead of one
project command. It reports profile, port, process, proxy, dashboard, and public backup
metadata without secret values, raw logs, credentials, execution contexts, or backup
recipient material. A stopped proxy/dashboard or unconfigured backup is a readable state;
`diagnostics.data.ready: false` means at least one subsystem could not be inspected safely.
Each remedy has `argv`, `required_inputs`, and `message`. Supply missing inputs before
execution; an empty argv describes a manual action. These are suggestions, not automatic
permission to install tools or change data. Keep secrets in stdin/files, never remedy argv.
Variables are readable; secrets are metadata-only and enter processes through
`command run` or managed commands. Import mixed dotenv files by path, marking public keys
with `--var`; new unmarked keys become secrets. Keep secret values out of arguments,
conversation, and logs. Child output and explicitly captured logs may contain secrets.

Use `command list` to discover configured commands, `command inspect NAME` to
inspect one, and `command run NAME` for foreground work. Use `process start NAME`
for a persistent server.
In devtools 0.22.2 and newer, command names are literal keys of 1–128 characters.
Punctuation, spaces, and Unicode are supported; leading `-` and control characters
are invalid. For example, `[commands."dev:docs"]` with `exec = ["pnpm", "run", "docs:dev"]`
is executed by `devtools run dev:docs`. Quote TOML keys containing punctuation and
shell arguments containing spaces or shell syntax. Profile, env, port, and proxy
names keep their separate identifier rules. Check `devtools schema command run`
for the installed command-name contract; CLI and Skill updates are separate.
On older protocol v5 builds such as v0.22.3, pass child arguments after an explicit separator:
`devtools run --env local dev:docs -- --port 3000`. This form also works on v6.
For CLI protocol v6 (devtools v0.23.0 and newer), put all devtools options before the configured command name:
`devtools run --env local dev:docs --port 3000`. Every token after the name is a
child argument, including `--env`, `--profile`, `--dir`, `--help`, and `-h`.
Use `devtools run --help` for devtools help and `devtools run dev:docs --help`
for the child's help. A single `--` immediately after the name remains an optional
separator; later `--` tokens are passed through. Direct execution still uses
`devtools run --env local -- PROGRAM ARG...`. The same rules apply to `command run`
and `cmd run`; other commands keep their existing option placement.
Save the returned execution ID. `process status` reports lifetime; a configured
`process wait EXECUTION_ID` establishes readiness before dependent work. `process check`
can exit successfully with `readiness.ready: false`.
Restart applies current config and values.

Use `project up COMMAND... --request-id UUID` for several named servers. Names are
required; never assume all configured commands are servers. Readiness waits are
per command. `project status [COMMAND...]`, `project logs COMMAND`, `project restart
COMMAND... --request-id UUID`, and `project down [COMMAND...] --request-id UUID` select
the current project's canonical directory, not other worktrees sharing its profile.
`project logs` reads the active captured output for that command; use execution-ID
`process logs` for ended history. Restart freezes the active execution IDs selected by
the first request, applies current project configuration and values, and never starts a
missing command implicitly. Omitted env/capture options inherit the execution's prior
selection. On partial failure inspect `error.details.items`; successful siblings stay
running. Retry the same input/UUID to continue unfinished children. A resumed batch may
return updated progress with `replayed: true`. Completed requests replay their saved
result. Down/restart retries keep the original execution targets. Use a new UUID for a
new operation and `project status` for current state.

Ports belong to execution locations. Inspect `port` and `instance` before changing
assignments. Commands declare `serve` for servers and `bind` for injected values.
Coordinate consumers when a stored port changes.

Use `proxy` when worktrees need stable `.localhost` hostnames instead of direct
port URLs. Routes come from each instance's tracked `[proxies.NAME]` declaration
and current port assignment. When a route uses `${instance.alias}`, give every
routed instance an alias with `instance name`. Allocate the referenced service
port, then inspect `proxy list` before starting the user-global daemon. Do not
start one daemon per project or worktree; route, alias, and assignment changes
are picked up on the next request.

`proxy start` and `proxy stop` require a request UUID. Reuse a UUID with identical
input only after an uncertain response. The listener port remains reserved after
stop; changing it requires starting the stopped daemon with an explicit new port.
Treat non-`ready` list entries as diagnostics to resolve, not fallback targets.
The proxy accepts only loopback HTTP traffic and routes only `.localhost` hosts
to stored local assignments; use the project's own TLS setup when HTTPS is needed.

## Plan, claim, recover

Use an independent task for a small job, or a workstream for a goal needing a
specification and plan. In a workstream, connect requirements, acceptance criteria,
tasks, and validations; set the plan's complete references, then check and activate.
Task dependencies stay within a workstream; workstream dependencies stay within a profile.

Use `task workstream edit` for atomic insertion, definition updates, scope removal,
restoration and ordering in any lifecycle state. Read its schema, preview with
`--dry-run --if-revision`, then submit the same body with a request UUID and the
observed revision. Preview does not consume a request ID. Incomplete coverage can
be saved; resolve returned issues before execution or closure. Removed tasks keep
history and claims; restoring them requires explicit dependency/acceptance links.
Legacy plan set arrays assert the complete included membership.

Before source work, claim the task and check `claimed` and `context_valid`.
Keep the returned context private; pass it explicitly or through
`DEVTOOLS_TASK_CONTEXT`. Public task/run IDs identify work but do not authorize it.
Claims coordinate records; coordinate overlapping files separately.

Checkpoint decisions, remaining work, next action, and evidence before a handoff.
Checkpoint and release target the run ID returned by claim or takeover; done and
sync target the task ID.
In a new session, use `task current --dir PATH` and `task context TASK_ID`,
then inspect the actual working tree. Resume with an existing valid context,
take over the observed active run using `--expected-run`, or claim released work.
Claims end through explicit actions, not elapsed time.

Check `completion_status` and `execution_status` alongside lifecycle `state`.
A done task with stale completion is claimable when ready. A stale current run
must review the changed definition and use `task sync` with context, reason and
observed revision before current validation/completion. Sync preserves the run
and does not create passing evidence. Checkpoint and release remain available.

## Retry and finish

Each task mutation needs a request UUID. Reuse the UUID and identical input after
an uncertain response; changed input gets a new UUID. For revision-guarded edits,
use the latest query revision. On conflict, refresh and reassess before resubmitting.

Create a code basis for the observed definition, perform validation, record its
evidence, then complete the task. Task basis requires the current execution
context; workstream basis requires the observed profile revision. Late records
stay on their run-owned basis and may return `applicable:false`.
The CLI stores evidence; it does not execute or verify the supplied evidence.
After takeover, explicitly accept reusable validation results for the new run.
Close a workstream after its task results and required integration checks satisfy
the acceptance criteria. Use waivers only within the user's agreed scope.
After a closed workstream becomes stale, explicitly close it again once current
evidence is complete. A later pass alone does not close it.

The first real task mutation upgrades a v1 journal to v2 atomically. All writers
sharing the profile must support v2; older binaries reject it. Read, no-op and
preview do not upgrade. Use error details and remedy argv to recover conflicts;
never copy a context credential into shared diagnostics.

Use `profile list`, `profile inspect NAME`, and `profile diff LEFT RIGHT` for metadata-only
profile management. Diff reports key/scope and task/instance differences, not stored
values or secret equality; no metadata difference does not prove equal values.

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

Cleanup and restore start with a preview. Apply the selected IDs or digest,
refreshing stale previews. Keep backup identities separate from project files.
Provide a dashboard link when the user needs visual management; its session can
modify all of the user's profiles. Creating a plan does not authorize unrelated
publishing, deletion, or changes to external systems.
