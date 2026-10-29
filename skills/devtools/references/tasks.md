# Requested task/workstream coordination

## Choose the record

- Use only when the user or project requires devtools coordination. Ordinary source work does not require task creation/claiming.
- Small independent job: task. Goal needing specification and plan: workstream.
- Workstreams connect requirements, acceptance criteria, tasks and validations. Set complete plan references, then check and activate.
- Task dependencies stay within a workstream; workstream dependencies stay within a profile.
- Discover each exact command schema. JSON bodies use `--file` or `--stdin`, not equivalent body flags together.
- Protocol 20 adds query-only `--query-cache-dir PATH`: an absolute private snapshot directory separate from the entire data root and its ancestors. Use the same directory for every cursor continuation; no automatic cache search or temporary fallback occurs. Discover support before passing it to an installed CLI.
- Linux/macOS initialized, clean task stores support public queries with read-only data/locks. New paginated lists/truncated graphs still need a writable cache. Single queries/cursor reads do not create or prune cache files; cached cursors are independent of current data storage. Windows locks retain their existing access contract.
- On `initialization_required`/`recovery_required`, run the suggested `task next` with write access to the same profile/isolated root, then retry the original query/options. This preparation may write storage. On `query_cache` failure select a writable private cache; never accept a missing cursor as complete pagination.
- Custom cache lifetime belongs to the caller. Publication opportunistically prunes only recognized expired snapshots in that directory; global cleanup does not register custom paths.
- Use bounded list/tree queries; follow cursors/continuations for full requested coverage. Cursors bind filters/revision; `cursor_invalid` requires a fresh query.

## Edit and claim

- `task workstream edit` atomically inserts, updates, removes/restores scope and changes ordering in any lifecycle state.
- Preview with `--dry-run --if-revision`; apply the same body with observed revision and a request UUID. Preview does not consume an ID.
- Incomplete coverage may be saved; resolve issues before execution/closure. Removed tasks keep claims/history; restoration needs explicit dependency/acceptance links. Legacy plan-set arrays assert complete included membership.
- Before source work on a managed task, claim and check `claimed` plus `context_valid`.
- Keep the returned context private; pass explicitly or via `DEVTOOLS_TASK_CONTEXT`. Public task/run IDs grant no authority. Claims coordinate records, not overlapping files.

## Resume and hand off

- Find existing work with `task current --dir PATH` and `task context TASK_ID`; inspect the actual working tree.
- Resume a valid context, take over the observed run with `--expected-run`, or claim released work. Elapsed time does not end claims.
- Check `completion_status` and `execution_status` as well as lifecycle state. Done-but-stale completion may be claimable when ready.
- A stale run must review its changed definition and `task sync` with context, reason and observed revision. Sync keeps the run; it creates no passing evidence.
- Checkpoint decisions, remaining work, next action and evidence before handoff. Checkpoint/release target the run ID; done/sync target the task ID.

## Validate and finish

- Each task mutation needs a request UUID. Response loss: retry identical input/UUID. New decision: refresh revision and use a new UUID.
- Create a code basis for the observed definition, perform configured checks, then record evidence. Task basis needs current execution context; workstream basis needs observed profile revision.
- Late evidence stays on its run-owned basis and may return `applicable: false`. After takeover, explicitly accept reusable validation results.
- The CLI stores evidence; it does not execute checks or verify claims made in evidence.
- Close only after task results and integration checks satisfy acceptance criteria. Waivers must remain within agreed scope.
- If closure becomes stale, explicitly close again after current evidence is complete; a later pass alone does not close it.
- The first real v1-journal mutation upgrades atomically to v2. All sharing writers must support v2; reads/no-ops/previews do not upgrade. Never copy context credentials into diagnostics.
