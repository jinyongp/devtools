---
name: devtools
description: Operate the devtools CLI for project setup, managed servers, stable local proxy routes, ports, and task/workstream coordination. Use when a repository has devtools.toml or when the user asks to inspect, configure, run, coordinate, or recover devtools-managed work.
license: MIT
compatibility: Requires the devtools CLI in PATH and filesystem access to the target project and the user's devtools data directory.
---

# devtools

## Discover only what you need

Start with `devtools <group> --help`. For structured input, request one contract,
such as `devtools schema task claim`. Bare `schema` lists groups; `schema --all`
is for explicit full-catalog work. Reuse discovered contracts during the session.
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
Use `version` for build metadata and `protocol_version` (3 for this source).
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
Save the returned execution ID. `process status` reports lifetime; a configured
`process wait EXECUTION_ID` establishes readiness before dependent work. `process check`
can exit successfully with `readiness.ready: false`.
Restart applies current config and values.

Use `project up COMMAND... --request-id UUID` for several named servers. Names are
required; never assume all configured commands are servers. Readiness waits are
per command. `project status [COMMAND...]` and `project down [COMMAND...] --request-id UUID`
select the current project's canonical directory, not other worktrees sharing its profile.
On partial failure inspect `error.details.items`; successful siblings stay running.
Retry the same input/UUID to continue unfinished children. A resumed batch may return
updated progress with `replayed: true`. Completed requests replay their saved result.
Down retries stop only executions selected by the original request. Use a new UUID
for a new operation and `project status` for current state.

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

Use `profile export` and `profile import` to move one encrypted profile between
environments. Export defaults to the current project's profile. `backup status`
reports configured public backup state. Create/export accept either `--recipient`
or `--recipient-file`, never both; omission uses the configured public recipient.
Transfer only the public recipient to the source and keep the identity on the destination.

Import without `--apply` previews and returns `digest`, `target_exists`, and metadata-only
`diff`. It infers a single source profile and keeps its name unless `--as` is supplied.
Review that preview, then apply the same file/source/target with
`--apply DIGEST --request-id UUID`. Existing targets require `--replace` at apply and
a configured safety backup. Stale targets require a fresh preview and a new request ID.
Identical apply retries replay the stored result; changed inputs conflict. Task claims,
execution credentials, port assignments, and live processes are not transferred as active state.

Cleanup and restore start with a preview. Apply the selected IDs or digest,
refreshing stale previews. Keep backup identities separate from project files.
Provide a dashboard link when the user needs visual management; its session can
modify all of the user's profiles. Creating a plan does not authorize unrelated
publishing, deletion, or changes to external systems.
