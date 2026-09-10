# Tasks, workstreams, and dashboard

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
For an uncertain response, retry with
the original request ID and unchanged inputs. A revised request gets a new UUID.
`DEVTOOLS_TASK_CONTEXT` supplies the default execution context.

## Plan a workstream

Create with `task workstream create --title TEXT --request-id UUID`. Document and
definition edits take `--if-revision N` from the latest query and a request ID.
JSON bodies use a regular file with `--file PATH` or redirected input with `--stdin`.
Command help lists options. Use `devtools schema task workstream spec set` to
retrieve the input and JSON body schema for that command.

Update existing workstream metadata with `task workstream update WS_ID`. Metadata
is the public concept; `title` and `description` are the initial supported fields.
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
devtools dashboard
devtools dashboard --json
devtools dashboard status
devtools dashboard stop
```

Lists return `next_cursor`. Graphs return a `cursor` and continuation roots when
truncated. Continue with the same cursor to keep the original revision for up to
30 minutes. A fresh query observes the latest profile revision.

`devtools dashboard` prints a clickable URL. Open the full link, including its
fragment, to connect the browser tab. Use `--json` for server metadata. Choose a
profile from the header's dropdown; controls become available after authentication.

The dashboard embeds D3 7.9.0 and renders directed dependencies on Canvas. The
keyboard-accessible item list opens context and workstream tasks. Use the profile
selector for the profile overview, and select a node to emphasize its connected
paths. The entry link is valid for five minutes and one use; the browser session
lasts eight idle hours or until the server stops.

Use the **Profile** selector to choose a profile. The central list opens item
details; switch to **Graph** to explore dependencies. Details separate overview,
documents, and activity. Agents claim tasks and record progress and completion
through the CLI. The dashboard manages plans, definitions, and interventions.

**Dependencies** selects prerequisites by title. Workstream **Specification**
edits requirements and acceptance criteria as rows, with selectable requirement
references. **Plan** selects tasks and validations belonging to that workstream.
**Cancel** previews the open tasks that will also be canceled. Apply uses the
previewed profile revision; concurrent changes require a fresh review.

For a running task, **Revoke claim** records a reason and invalidates the observed
run's execution context. It leaves the task open. Running commands continue until
stopped separately; coordinate with the agent before assigning the work again.
The equivalent CLI command is:

```sh
devtools task unclaim TASK_ID --expected-run RUN_ID --if-revision REVISION --reason "Agent session ended" --request-id UUID
```

Use `task show TASK_ID` to obtain the current run and profile revision. After
revocation, a new CLI `task claim` starts a new execution. Late writes using the
previous context return `context_invalid`; the revocation reason remains in history.

Dashboard navigation is stored in the URL query: profile, workspace tab, environment,
search and filters, list/graph view, selected item and its detail tab, expanded process
details, and graph position. Reloading restores the current view. Browser Back and
Forward restore navigation; typing a filter updates the current history entry.
Values and secrets, edit forms, imported content, and raw logs stay in memory.
Authentication remains in the current browser session, so a view URL also requires
an authenticated session. If an environment was removed, the view returns to Common.

The visible dashboard checks for changes every five seconds and redraws when its
data changes. Polling pauses while the browser tab is hidden, a dialog or inline
editor is open, or an input has focus. Returning to the tab checks immediately.
Raw logs and validation evidence are loaded through their explicit controls.

**Variables & secrets** selects common values or an environment. Variables display
their values; secrets display storage metadata and accept a replacement value.
Editing an inherited value creates an override in the selected environment.
From **Common**, choose **Override in…** on a key, select an environment, and enter its value. Existing overrides are identified before saving. Saving opens that environment; the common value stays unchanged. Create an environment first if none exists. Secrets accept a new value with a masked input.

With an environment selected, **Inherited common**, **Overrides common**, and
**Env only** distinguish storage layers. Inherited rows use a muted background;
editing them offers **Create override**. **Remove override** restores inheritance,
while **Delete** removes an environment-only value. These labels apply equally to
variables and secrets and reflect stored layers, even when values are equal.
Use the Key, Kind, and Source column headers to search keys and filter by kind
and inheritance status together. Click a value to edit it in place,
then save or cancel; secret cells accept a replacement. **Import .env** accepts
a file or pasted assignments and previews key names and actions. New keys default
to secret; select public variables in the preview. **Overwrite existing values**
writes to the selected layer. Keeping existing values skips effective keys,
including inherited common values. The import is applied atomically.
Each import accepts up to 500 KB and 5,000 keys.
Removing an override restores the common value. Concurrent changes require a
refresh before editing again. See the [management API](management-api-contract.md)
for request and retry behavior.

## Storage and dependencies

Task journals live under the `tasks` subdirectory of the data path reported by
`devtools project inspect`. Files are private to the user. The dashboard server
registry uses the cache directory. Query snapshots are stored under
`task-queries` for CLI queries and `dashboard/queries` for dashboard queries,
relative to the cache path. Sessions and the dashboard response cache live in
server memory. The browser keeps its credential in origin-scoped sessionStorage and
sends it explicitly in the Authorization header. Task records and execution context
receipts are separate from the variable/secret store.

The project pins Go 1.27.1 and go-toml 2.4.3. The dashboard includes the exact
official D3 7.9.0 bundle, with its license and SHA-256 in
`internal/dashboard/assets/dependencies.json`. D3 declares no peer dependencies.
CI actions use immutable commit references and just uses version 1.58.0.

`just verify-docker` runs Go race tests and installed Linux scenarios for
concurrent claims, takeover, retries, worktree sharing, and dashboard sessions.
The sandbox runs the compiled release with a private test home.
