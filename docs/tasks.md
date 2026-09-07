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
in the same profile. Every prerequisite must be done before execution proceeds.

## Recover and verify

Use `task current --dir PATH` and `task context TASK_ID` to inspect saved work.
`task takeover TASK_ID --expected-run RUN_ID --request-id UUID` creates a new run
and context. The previous context becomes inactive.
`task release RUN_ID --context CONTEXT --request-id UUID` ends a claim explicitly,
and `task resume --context CONTEXT --request-id UUID` continues an existing context.

Validation definitions describe the method and required evidence. Create a basis
with `task validation basis VAL_ID --file FILE --request-id UUID`:

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

Use **Choose profile** to select an existing or new profile identifier. The graph
view offers workstream/task creation and editing actions in item details, including
dependencies, specification, plan, claim, takeover, checkpoint, and completion.
Execution contexts stay in page memory; after a reload, review the checkpoint
and take over the current run to continue. CLI claim and transition checks apply.

**Variables & secrets** selects common values or an environment. Variables display
their values; secrets display storage metadata and accept a replacement value.
Editing an inherited value creates an override in the selected environment.
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
