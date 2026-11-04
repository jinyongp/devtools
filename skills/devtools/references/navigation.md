# Collection navigation and executable recovery

Discover the installed protocol and command schema first; these are protocol 23
capabilities. Default collection size is 50, maximum 200. Never interpret one page
as a complete inventory. Follow the actual continuation argv with unchanged root,
profile/directory, view, filters, sort and query cache. Cursors expire in 30 minutes.

- `process list` defaults to current services keyed by profile + canonical directory
  + command. Current execution and last attempt are separate; failed preparation
  may coexist with a running server. Conflicts expose every active execution ID.
  Use `--view history` for execution rows. Stop/status/restart/logs take actual
  execution IDs, never a current service key or an unproven request ID.
- Protocol 24 adds foreground executions and `--mode foreground|managed` to process
  queries. Apply mode before service aggregation. Control capability, readiness,
  lifetime and owned-scope cleanup are distinct; lost controllers remain uncertain.
- `expose list` and `values execution list` default to current/unresolved work;
  resolved history requires `--view history`. Unknown/in-flight effects remain.
  Existing task completion/stale/scope semantics are preserved.
- Project collections default to the current canonical project directory.
  `--profile` covers that profile across locations; `--dir`/`--instance` intersect.
  `--all-profiles` is explicit global selection. Outside projects, resolve
  scope_required through the supplied profile/instance lookup. Originally global
  resources such as profile list/archives stay global. Respect proxy --global.
- Repeated state options are OR; different conditions are AND. Search covers
  public names/commands, not values/logs. Time windows are `[since, until)` using
  RFC3339 with timezone and a schema-advertised recorded time field. Missing end
  times are not inferred from file mtime.
- Selection precedes stable sort and snapshot paging. Read-only data can use a
  separate private absolute `--query-cache-dir`. Keep it on every continuation.
  One-page results create no cache; cursor reads never create/prune. Cache failure
  is not a completed empty result. Caller-selected cache locations remain caller-owned.
- `--columns` is repeated, unique and limited to published text columns. Request
  `--format text` explicitly; effective JSON rejects columns and keeps required fields.

Recovery actions have state `ready`, `needs_input` or `manual`. Only ready argv is
a complete invocation. Ordinary new-operation guidance omits `--request-id`; the
CLI generates and returns it at execution. Replay and preview-bound apply actions
preserve their bound UUID and input. Do not replace known IDs, revisions, digests or root.
Missing private input has no fake executable argv: read `inputs`, follow the real
`input_steps`, and preserve public `bindings` when supplying it. Never reconstruct
stdin, request secrets in chat, or open an implicit prompt in agent/CI/pipe mode.
Root options precede the command; command flags precede child `--`.

Preview apply actions retain supplied public/child input and its digest/revision.
Changed files/input require a fresh preview; a ready action is not authorization
for an unrelated mutation. General help examples are templates, not observed actions.

Related mutations perform bounded owned maintenance and save a resume position.
Ordinary queries never expire/delete artifacts. Use `cleanup maintain --dry-run`
to inspect the next pass and the same command without dry-run for an authorized
pass. Resolved detail history is 30 days, captured logs 7 days, query cache 30
minutes. Live/unknown/recovery references and missing historical end times stay.
Request identity and exact replay/restore bodies retain their original lifetime;
shared compression does not imply constant total storage. User tasks/definitions
have no TTL, and manual archives retain explicit restore/purge with the 30-day guard.
Locked protected maintenance reports verification pending rather than unlocking.
Storage format/query lock preparation belongs to `doctor --storage --fix` and the
existing installation/update hook; that preparation does not perform TTL cleanup.
