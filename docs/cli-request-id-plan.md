# Optional CLI operation request IDs

Baseline: `52d914aeba4d05fced77518b5d5a0133bf29a886` on main.
Scope: omit `--request-id` for new CLI operations, preserve explicit retry IDs,
and simplify ordinary examples. No release, user-data cleanup, new request store,
or change to the domain APIs' retry and authorization requirements.

## Investigation

The complete 199-command registry contains 81 `--request-id` options:

- 67 always-required operation IDs: process/project lifecycle, task mutations,
  proxy/network mutations, cleanup apply, vault rewrap, bundle mutations,
  generated secrets, runner registration and value-execution controls.
- 13 conditional operation IDs: seven guarded value/env/import commands,
  workstream edit, proxy setup/teardown, external exposure start, backup restore
  and profile import.
- One query selector: `values execution status --request-id` locates an existing
  preparation. It must never become a newly generated operation ID.

Positional IDs in request inspection commands are existing-resource selectors
and remain required. Dashboard clients already generate and retain explicit IDs;
their domain APIs continue to require them. `run` has its own foreground execution
semantics and does not acquire a new mutation option.

Relevant shared boundaries are `Option`/`Command`, registry finalization,
`parseRequest`, `staticContract`, `App.Run`, `outputController`,
`publicResultField`/`publicResultObject`, `ResolveAction`, and the reviewed-example
catalog. `parseRequest` is also used for action validation: it must remain pure
and must not create requests merely to validate or describe a command.

Existing domain receipts bind ID and input, replay a completed result, and reject
different input under the same ID. `protocol.NewRequestID` already supplies UUIDs
for prepared actions. Neither mechanism needs another implementation or store.

## Decisions

1. Describe operation IDs once using a common option constructor and optional
   trigger metadata. Reuse the existing static input predicates. The selector
   option has no generation policy.
2. Generate one ID at the actual CLI execution boundary when the operation's
   trigger matches and the caller omitted it. Preserve an explicit valid ID.
   Do not fabricate an argv source offset for an automatically supplied value.
3. Expose the effective operation ID at the public success/error boundary and
   retain it for output-encoding/render/transport failures. Reuse existing domain
   journals and existing prepared recovery actions. Never automatically retry.
4. Keep revision, expected-run, apply digest, authentication and body-source
   requirements. A guarded value mutation still requires a caller-selected
   revision; an explicit request ID does not authorize a preview or missing guard.
5. Preview/check/query paths do not admit operations or create receipts. Existing
   bundle preview validation may use an in-memory ID required by its domain API;
   this does not consume an ID or create another operation journal.
6. Make parser, static rules, published schemas, help and action validation agree
   about the optional ID. Remove obsolete paired-ID requirements at their owning
   definitions instead of adding bypasses in individual handlers.
7. Ordinary examples omit the operation ID. Optional flags remain in examples
   whose stated purpose demonstrates them. Explicit retry/lookup examples retain
   the original ID and necessary scope. Prepared recovery/apply actions retain
   their bound IDs so copying the same action preserves its identity.
8. Keep the unreleased protocol-23 source contract; envelope and persisted receipt
   formats do not change. Installed older contracts retain their own requirements.

## Design review before implementation

| Finding | Required resolution |
| --- | --- |
| Generating in the parser would also mint IDs during schema/action validation | Generate only when executing a CLI operation |
| Existing oneOf/static rules require UUIDs even after an option becomes optional | Revise the owner of each conditional contract and recheck both raw input schema and normalized execution |
| Revision+ID guards can accidentally become unguarded writes | Generate only with the supplied revision; retain reverse ID→revision and create-only protection |
| Apply+ID guards can accidentally authorize preview application | Apply digest remains required; explicit ID alone remains invalid |
| Existing success results expose IDs inconsistently | Add invocation metadata through the common public result boundary, without changing stored results |
| Render failure could lose a generated ID after a successful write | Bind the output controller to the effective ID before invoking the handler |
| Hidden recovery inputs must remain private | Preserve original scope/revision/ID bindings and existing acquisition guidance |
| Removing all optional flags would erase deliberate feature examples | Minimize first-use examples and keep clearly introduced variants |
| Existing negative tests treat omission as invalid | Replace those expectations with meaningful missing-guard/conflicting-input cases, retaining explicit-ID replay checks |

## Work and completion criteria

| Area | Initial review | Completion evidence |
| --- | --- | --- |
| Registry and parser | reviewed and rechecked | All 80 operation-ID options optional; selector and positional lookups unchanged; invalid explicit IDs rejected |
| Shared generation and output | reviewed and rechecked | Shared execution boundary, existing UUID generator; success/error/transport identity; canonical vault IDs preserved; text returns the ID |
| Conditional inputs and previews | reviewed and rechecked | Guarded revision, apply digest, workstream preview, setup check and local exposure retain their protections |
| Domain receipts and recovery callers | reviewed and rechecked | Real process/task/value replay and conflict checks; preview does not consume a receipt; prepared actions retain bound IDs |
| Help/schema/completion/action resolver | reviewed and rechecked | All 337 published examples parse and validate; discovery/validation creates no operation |
| Examples/docs/skill | reviewed | Removed new-operation UUID placeholders from 111 examples; simplified first-use defaults; retained lookup and retry guidance, HTTP API and historical contracts |
| Installed behavior and native checks | reviewed; Linux verified | Full project checks with race detection and all 39 Linux installed scenarios pass; existing Unix/Windows workflows are the final native platform gates |
| Integrated review | reviewed and rechecked | All source, callers, schemas, examples and bundled skill changes reviewed; confirmed findings fixed; no unresolved source finding; exact-head platform results reported with the task |

Use existing checks first. Add coverage for omitted-ID process/task/value writes,
same-ID replay and conflict, render/transport failure, conditional previews and
query selectors, and registry-wide parser/schema consistency. Exercise at least
one installed start→stop and interrupted/replayed operation using returned IDs.
Keep explicit-ID regression fixtures to protect deterministic retry behavior.
Validate on main using the existing project checks and GitHub workflows; do not
create a worktree or modify workflow permissions or release triggers.

## Review loop

1. Replaced the obsolete paired-ID input branches with the shared operation
   policy. Removed redundant setup/teardown/import/restore handler checks; lower
   domain APIs retain their explicit-ID validation and existing receipts.
2. Updated negative tests that previously rejected omission. Their replacement
   cases cover malformed UUIDs, missing revisions/apply authorization and
   conflicting stdin/file/authentication options. Explicit retry fixtures remain.
3. The first full CLI pass found existing vault rewrap lowercase normalization
   being overwritten by public metadata. Preserve the producer's canonical ID;
   its existing uppercase-input lifecycle test now passes.
4. A real text task invocation exposed a hidden generated ID. Add its presentation
   once at the shared purpose-view boundary and retain existing uncertain-outcome
   recovery notes. The terminal ID now successfully replays the same task.
5. The focused regression pass covers registry inventory, raw published schemas,
   pure action validation, conditional execution, real managed start/stop, guarded
   values/env/import, task and profile import replay/conflict, vault rewrap,
   render/transport/encoding failure and the complete example catalog. It passes
   in `/tmp/devtools-request-id-presentation-2.log`.

6. The complete project pass also identified proxy negative fixtures that still
   expected missing IDs to fail. Replace those inputs with malformed UUIDs; actual
   omitted-ID start/stop behavior is covered by real managed/installed journeys.
7. Installed recovery compares unchanged persisted domain results plus CLI-only
   invocation metadata. Preserve exact result and receipt-hash assertions rather
   than changing the stored format. Secret-generation output whitelists now allow
   exactly the existing metadata plus request_id; no plaintext output is allowed.
8. A real committed task with a broken stdout transport returns the generated ID
   in its diagnostic. Retrying that ID replays the receipt and leaves one task.

## Validation evidence

- `/tmp/devtools-request-id-final-gaps.log`: affected replay, transport, text,
  normalization and malformed-ID regression checks pass.
- `/tmp/devtools-request-id-project-check-2.log`: `./scripts/project check -p=1`
  passes on main, including all required static checks and Go race tests.
- `/tmp/devtools-request-id-installed-3.log`: `python3 -B verify/run.py all`
  passes all 39 installed-release scenarios with disposable native Linux roots.
  The final fixture updates exercise generated IDs for process/project recovery,
  task creation, guarded values and secret generation, including native Windows
  product assertions that the existing Windows workflow runs.
- Public discovery reports 199 canonical commands and 81 optional request-ID
  options. The registry test distinguishes the 80 operation policies from the
  unchanged query selector; all 337 published examples validate.
- Existing full `unix-platform.yml` and `windows-platform.yml` workflows must
  finish successfully on the pushed main commit. Their exact-head URLs/results
  are reported with the completed task; no workflow or release trigger changes
  are needed. Optional live-provider/performance gates retain their own opt-in.

Initial worktree was clean. The change owns 67 modified files and three new files
(this plan, the common operation-ID policy and its focused tests), with no deleted
files. No domain journal, runtime dependency, global installation, worktree or
release was added.
