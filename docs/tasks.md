# Tasks and workstreams

[Documentation index](README.md) · [Install and get started](../README.md)

`devtools task` stores project work in the user's global profile store. A tracked
`devtools.toml` selects the profile across checkouts and worktrees; `--profile NAME`
selects it explicitly. Each successful mutation appends actions atomically and
returns the resulting profile revision. States are projected from those actions.

## Start with one task

Run these commands in a project with `devtools.toml`, or add `--profile NAME`
to each command. To prepare a new directory, use `devtools init --profile myapp`.

```sh
devtools task add --title 'Review installer failure handling' --request-id UUID
devtools task next
devtools task claim TASK_ID --request-id UUID
devtools task checkpoint RUN_ID --summary 'Checked rollback; update tests next' \
  --context CONTEXT --request-id UUID
devtools task done TASK_ID --summary 'Rollback verified' \
  --context CONTEXT --request-id UUID
```

Replace each new `UUID` with a generated UUID, for example the output of `uuidgen`.
Use `data.item.id` from add as `TASK_ID`. A successful claim returns
`data.claimed: true`, `data.run.id` as `RUN_ID`, and `data.context` as `CONTEXT`.
Check `data.context_valid` before continuing; an unsuccessful claim explains its
blockers. Keep the context private and use the same profile for every step.
Every committed mutation reports `previous_revision`, `revision`,
`current_revision`, `affected_ids`, and `affected_count`. Treat exit code 0 as a
committed change only when `changed` is true and `affected_count` is positive.
A definition or state update that changes nothing fails with `no_change` and exit
code 3; it creates no action, receipt, or revision. Appending a checkpoint or result
is itself a change. Empty automatic claims and dry-runs remain
intentional non-changing successes.
For an uncertain response, retry with
the original request ID and unchanged inputs. A revised request gets a new UUID.
`DEVTOOLS_TASK_CONTEXT` supplies the default execution context.
Only commands that consume execution context or optionally use it as a guard
expose `--context`. Context-free mutations ignore ambient context and do not
bind it to their retry receipts. Receipts written by older versions are
normalized to the current revision-boundary and affected-item response fields.

`task update` can run without a claim context as an administrative definition
edit. If `--context` or `DEVTOOLS_TASK_CONTEXT` supplies one, devtools validates
that it belongs to the current run for the target task. A rejected context uses
`context_invalid` with `details.context_reason` set to `missing`, `unknown`,
`inactive`, or `target_mismatch`, without returning the credential. Target
mismatches also report `expected_target_kind` and `actual_target_kind`.
Task-owned validation basis, record, accept, waive, and unwaive operations use
the same reason codes. Workstream-owned validations do not consume task context.

A checkpoint changes execution history, not the task definition. Its success
advances the profile revision, updates the task/run projection, and names the
task in `affected_ids`; the task's `definition_revision` stays unchanged. Read
the saved payload with `task checkpoint list RUN_ID` or task history.

## Plan a workstream

Create with `task workstream create --title TEXT --request-id UUID`. Document and
definition edits take `--if-revision N` from the latest query and a request ID.
JSON bodies use a regular file with `--file PATH` or redirected input with `--stdin`.
Command help lists options. Use `devtools schema task workstream spec set` to
retrieve the input and JSON body schema for that command.

Update existing workstream metadata with `task workstream update WS_ID`. Metadata
is the public concept; `title` and `description` are supported fields.
Provide at least one field with the latest revision and a request ID. An empty
description clears it. Use the `workstream.update` operation of `task workstream
edit` when metadata must change atomically with other plan content.

The command names below describe individual steps. Every mutation takes a new
`--request-id UUID`; document and definition edits also take `--if-revision N`.
For example, save the following specification as `spec.json`, read `data.revision`
from `task workstream show WS_ID`, and run:

```sh
devtools task workstream spec set WS_ID --file spec.json --if-revision N --request-id UUID
```

`task workstream spec set WS_ID` accepts:

```json
{
  "body": "Allow an agent to recover interrupted work.",
  "requirements": [{"key": "R1", "text": "Progress survives a session ending."}],
  "acceptance": [{
    "key": "A1",
    "requirement_keys": ["R1"],
    "text": "Another run can continue from a saved checkpoint."
  }]
}
```

In JSON bodies, add tasks with `workstream_id` and `acceptance_keys`
(CLI flags: `--workstream WS_ID --acceptance-keys A1`), then add validation definitions
with one owner: `task_id` or `workstream_id`. `task workstream plan set WS_ID` takes
a Markdown `body`, all `task_ids`, and all related `validation_ids`.
`workstream check` identifies coverage gaps; `workstream activate` enables execution.

Set prerequisite IDs with `task depends set TASK_ID --file FILE` or
`task workstream depends set WS_ID --file FILE`, using `{"depends_on":["UUID"]}`.
Task edges connect tasks in the same workstream; workstream edges connect goals
in the same profile. Every prerequisite must have current completion before execution proceeds.

## Edit plans at any lifecycle state

Use `task workstream edit WS_ID --file edit.json --if-revision N --dry-run` to preview
an atomic batch, then use the same body and revision with `--request-id UUID` to save.
The body contains `reason` and 1–200 `operations`. Insert, update, remove, restore,
change dependencies and reorder tasks without reopening completed work first.
See [the edit API](task-edit-api.md) for operations and examples.

Removal preserves history and claims. Restoring a task requires explicit acceptance
and dependency links. Coverage gaps can be saved and are checked before execution
or closure. Legacy plan-set arrays assert the complete included membership.

Read `completion_status` and `execution_status` alongside lifecycle `state`.
A ready done+stale task can be claimed atomically. A stale running task uses
`task sync TASK_ID --reason TEXT --if-revision N --request-id UUID` with its current
context after reviewing the changed definition. Sync creates no passing evidence.
`task list --scope removed` finds excluded history; `--completion stale` finds work
whose prior completion no longer covers its definition. `workstream plan show`
accepts `--at-revision N` for a complete historical request boundary.

## Recover and verify

Use `task current --dir PATH` and `task context TASK_ID` to inspect saved work.
`task takeover TASK_ID --expected-run RUN_ID --request-id UUID` creates a new run
and context. The previous context becomes inactive.
`task release RUN_ID --context CONTEXT --request-id UUID` ends a claim explicitly,
and `task resume --context CONTEXT --request-id UUID` continues an existing context.

Validation definitions describe the method and required evidence. Create a basis
with `task validation basis VAL_ID --file FILE --request-id UUID` and the current
task execution context. For a workstream validation, supply `--if-revision N` instead:

```json
{"code":[{"repository":"/workspace/project","commit":"COMMIT","dirty":false,"evidence":"git status and revision checked"}]}
```

Record the observed result through `task validation record VAL_ID --file FILE`,
including `basis_id`, `result`, `summary`, and `evidence` objects with `kind`,
`reference`, and `description`. Task-owned results require the current context.
The CLI records the evidence supplied by the executing agent.

Required validations need a passing result for the current basis or an explicit
waiver. An inherited pass uses `validation accept` to record reuse in the new run.
`done` completes the task; `workstream close` checks tasks and integration evidence.
`history` and `export` retain the public record for later recovery.

## Explore

```sh
devtools task list --state all --limit 50
devtools task tree TASK_ID --direction upstream --depth 3
devtools task workstream tree --direction downstream
```

Lists return `next_cursor`. Graphs return a `cursor` and continuation roots when
truncated. Continue with the same cursor to keep the original revision for up to
30 minutes. A fresh query observes the latest profile revision.
Graph depth accepts 0 through 20. Depth 0 returns only the selected root nodes,
without dependency edges or continuations beyond those nodes. Follow each emitted
continuation's ID, direction, and depth with the graph cursor; a depth-0 continuation
returns its boundary node and finishes that branch.

Graph cursors freeze node views (including run and completion status) and graph
relations at the first query revision. Follow-up queries do not replay event
history. Cursors from the older event-based cache format return `cursor_invalid`;
start a fresh query. This cache change does not migrate task journals or WALs.

Open [the Dashboard guide](dashboard.md) for login, plan editing, values, navigation and server controls.

<a id="storage-and-dependencies"></a>

## Storage and verification

Task journals live under the `tasks` subdirectory of the data path reported by
`devtools project inspect`. Files are private to the user. The dashboard server
registry uses the cache directory. Query snapshots are stored under
`task-queries` for CLI queries and `dashboard/queries` for dashboard queries,
relative to the cache path. Sessions and the dashboard response cache live in
server memory. The browser keeps its credential in origin-scoped sessionStorage and
sends it explicitly in the Authorization header. Task records and execution context
receipts are separate from the variable/secret store.

For isolated regression commands and native Windows checks, see
[installed-release verification](../verify/README.md). The
[development guide](development.md) covers build dependencies and contributor checks.
