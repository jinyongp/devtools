---
name: devtools
description: Use the devtools CLI to manage project variables and secrets, back up and restore profiles, plan workstreams, coordinate claimed tasks across agent sessions, record verification, and inspect dependency graphs.
---

# devtools

Discover the installed contract with `devtools schema` or a command's `--help`.
Commands return one JSON envelope: read `data` on success and `error.code` on failure.
Select a profile through the project's tracked `devtools.toml` or `--profile`.
Worktrees sharing that profile see the same workstreams, tasks, and history.

Use `devtools doctor` when preparing a project or diagnosing its environment.
For a named command, use `devtools doctor COMMAND` with the intended env.
Read `data.ready` and each check's `status` and `remedy`; diagnosis can return
successfully with `ready: false`. Requirements declare exact tool versions and
var/sec key presence in `devtools.toml`. Named `run` commands enforce declared
requirements before execution. Version probes execute the configured tool
arguments and keep their output and profile values out of diagnostic responses.

## Local ports

Use `port list`, `port show NAME`, and `instance list` to discover stored local
TCP assignments. `instance name ALIAS` names the current project location;
cross-project bindings select its profile and instance alias explicitly.
Commands declare `serve = ["api"]` for servers they start and `bind` for values
they consume. `doctor COMMAND` previews the checks; `run COMMAND` allocates
missing serve ports and prevents concurrent runs of the same service.
Assignments persist until release or prune. A busy stored port is an error;
coordinate an explicit release/reallocation and restart consumers when changing it.
Use `${var.KEY}` and `${bind.NAME}` in binding templates; exec arguments support
`${bind.NAME}`. Binding output overrides variables, and a secret-name collision
fails. Template values stay out of diagnostics. Check the CLI schema for options.

## Plan and coordinate

Use a workstream for a goal with a specification, implementation plan, and related tasks.
Create its draft, set the specification with stable requirement and acceptance keys,
add tasks and validation definitions, then set the plan's complete task and validation references.
`task workstream check WS_ID` reports coverage gaps; `activate` makes covered tasks claimable.
Independent tasks support small jobs with their own acceptance conditions.

Task dependencies stay inside one workstream. Workstream dependencies link goals within a profile.
`task next` explains the oldest ready task. `task tree` and `task workstream tree` traverse
upstream or downstream; follow returned continuations with the same graph cursor.
Use `--state all` to include completed and canceled items in lists.

Every task mutation requires a fresh UUID `--request-id`. Preserve it while resolving an uncertain
response and replay the exact request. A revised decision uses a new request ID.
Definition, document, relation, and lifecycle edits require `--if-revision` from a recent query.
On `revision_conflict`, read the current state and reassess the edit.

## Execute and recover

Claim a task with `task claim TASK_ID` or choose ready work with `task claim --workstream WS_ID`.
Save the returned execution `context` privately and pass it as `--context` or
`DEVTOOLS_TASK_CONTEXT`. The context authorizes the current run; general notes and dashboard
content should contain the public task/run IDs and progress, keeping the context private.

Use separate worktrees for concurrent source changes. Claims coordinate task records;
check overlapping files and running processes when splitting implementation work.
Record useful recovery points with `task checkpoint RUN_ID`: summary, decisions, remaining work,
next action, blockers, and validation references. `release RUN_ID` returns the task to the queue.

For a new session, find execution with `task current --dir PATH`, read `task context TASK_ID`
and relevant checkpoints, inspect the actual worktree and processes, then use
`task takeover TASK_ID --expected-run RUN_ID` to receive a new context.
An existing context continues through `task resume`; a released task starts through `claim`.
Claims remain until an explicit action ends or transfers them.

## Verify and finish

Define each validation's owner, method, acceptance references, and whether it is required.
Perform the verification in the project's agreed environment, then create a
`validation basis VAL_ID` describing the observed code state and record evidence against its
`basis_id`. Task-owned results use the current execution context.
The CLI records evidence; the executing agent checks that it matches the actual code.

When taking over, explicitly accept reusable passing evidence with
`validation accept VAL_ID --basis BASIS_ID --record RECORD_ID --reason TEXT`.
Use a reasoned `waive` only when the user's accepted scope permits the exception.
Complete the claimed task with `done TASK_ID --summary TEXT` and close the workstream after
its tasks and required integration validations satisfy its acceptance criteria.
History and export preserve the recovery record. Reopen follows dependency guards.

## Variables, secrets, and dashboard

Variables are readable through `var`; secrets use `sec` metadata and process injection.
Use `devtools run -- ...` or a configured project command to inject values into the child process.
For mixed dotenv migration, use `import --file PATH` and identify public variable keys;
the default classification protects the remaining values as secrets.

`devtools dashboard` returns a short-lived entry link for a read-only local D3 Canvas graph.
Pass the link to the user when a visual overview helps. Profile selection and node expansion
request their own scope. `dashboard status` and `dashboard stop` manage the local server.

## Back up and restore

Use `backup create` for all stored profiles, or select one with `--profile`.
Configure the destination and public recipient with `backup configure`; creation
also accepts explicit `--output` and `--recipient-file`. Keep the private identity
in the user's separate key storage and pass its file path only for inspection or recovery.

`backup inspect --file PATH --identity-file PATH` returns metadata.
`backup restore` previews the source `--profile` and destination `--as`.
Apply the reviewed digest with `--apply DIGEST --request-id UUID`, preserving
both inputs for retries. Existing targets require `--replace` in preview and apply,
and a configured destination/recipient for the automatic safety backup.
On `revision_conflict`, obtain a fresh preview and reassess the target.
Restored tasks preserve history and use new claims; reconnect project paths and ports.
