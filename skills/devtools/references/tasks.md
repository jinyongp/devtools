# Task/workstream coordination

## Plan, claim, recover

Read the exact schema before supplying a task body. JSON bodies use --file or
--stdin; do not combine them with equivalent body flags. Use bounded list/tree
queries and follow next_cursor/continuations for requested coverage. Cursors bind
filters and revision; cursor_invalid requires a fresh query, not a claim that the
partial list was complete.

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

Before source work on a devtools-managed task, claim it and check `claimed` and `context_valid`.
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

For configured validations, create a code basis for the observed definition,
perform the check and record its evidence before task completion. Task basis requires the current execution
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
