# Secret automation and approved local execution

[Documentation index](README.md) · [CLI contract](cli-contract.md) ·
[Local protection](secret-protection.md) · [Output protection](output-protection.md)

Use opt-in mutation guards and an approved execution interface for cooperating
runners owned by the same OS user. Check
`devtools version` and the installed command's `schema` before using them.
CLI protocol is 9; JSON envelope, storage and key-session versions
remain 1, 1 and 3 respectively.

Devtools owns value storage, key sessions, frozen input preparation and delivery
validation. The caller owns user authorization, job approval, isolation, network
policy, engine deduplication and cancellation. Selecting a profile, env, bundle or
key does not grant permission to execute. A delivery receipt proves neither
application consumption nor successful authentication to an external service.

Use mutation guards to change stored values safely. Use approved execution when
a caller-owned engine needs a frozen input for one authorized job. The latter
requires an existing protected data root even when all selected values are public
or the explicit selection is empty. The reference receiver is built from this
checkout; it is not an installed Docker or remote CI integration.

## Retry a mutation safely

Read `values revision --profile app`, then pass its opaque revision together with
a fresh `--request-id UUID` to a supporting writer. The two options are paired.
All writers share the storage commit conflict rules, including Dashboard and
legacy CLI paths; an unguarded legacy call cannot express the caller's earlier
editing intent. A guarded stale request fails with `revision_conflict`.

Keep the UUID, revision and exact logical input until recovery completes. After
response loss, retry that same request. A completed receipt is checked before the
old revision, so an intervening writer does not prevent recovery. Preserve the
UUID's exact spelling, including case, for mutation requests. Different input
under the same ID fails with `request_conflict`. The mutation and its successful
receipt publish in one transaction. Failed validation does not consume the ID.

`--create-only` rejects an existing exact slot, including an empty or expired
slot. `secret generate KEY --revision REVISION --request-id UUID` generates
32 random bytes encoded as 43 unpadded base64url characters. It returns metadata,
never the new value. Its retry returns the original result without generating a
replacement. `--replace` permits secret upsert: it also creates a missing slot,
preserves an existing live slot's expiry, and rejects an expired slot. It cannot
change a public variable into a secret.

`values request show UUID` passively authenticates the saved mutation result. It
requires an already unlocked cached session in a protected root and does not
start a daemon, unlock or renew idle expiry. Public-only roots without vault
protection can authenticate their mutation receipts without a key session.
Saved results describe the original operation; inspect current state
separately. New receipts permit replay for 30 days from creation. Afterward,
`request_expired` prevents the ID from becoming new work. Spent metadata stays for
the data-root lifetime. Older receipts without an expiry retain their existing
unlimited replay behavior. See [the CLI contract](cli-contract.md) for supported
writers, env layering and input byte rules.

Root deletion, restoring a complete historical root or moving a profile to
another root ends this local deduplication scope. Keep the original root and
caller request metadata available during recovery.

## Prepare and approve a local execution

Initialize and explicitly unlock existing protection first; follow
[secret protection](secret-protection.md). Execution control bodies contain only
metadata. Send them through exactly one of `--stdin` or `--file`; explicit nulls,
unknown fields and malformed IDs fail. Execution and runner request UUIDs use
canonical lowercase spelling. Preserve all request bodies and IDs in the caller's
own metadata storage. Never put a value or passphrase in those IDs or bodies.

Before starting, discover `devtools schema values execution prepare` and
`devtools schema values runner register`. Create the profile, selected keys and
any env or bundle first. Build and start the [reference caller](#run-the-reference-caller)
or an implementation of the private receiver protocol before registering it.
Use the same devtools data root for both processes; `--root` takes the data
directory itself, not the project directory or its `vault` subdirectory.

1. Start the caller's receiver with a new process instance UUID. Obtain its
   computed transport metadata using `values runner address RUNNER_ID --instance INSTANCE_ID`.
   Register the live instance with `values runner register --request-id UUID
   --stdin` and body
   `{"runner_id":"RUNNER_ID","instance_id":"INSTANCE_ID","protocol":1,"redaction_protocol":1}`.
   Registrations are data-root wide and have no profile option. Disable an observed
   old instance using `values runner unregister RUNNER_ID --instance INSTANCE_ID
   --request-id UUID` before registering a successor.
2. Call `values execution prepare --profile app --request-id UUID --stdin` with
   `{"external_execution_id":"job-42","selection":{"keys":["TOKEN"]},"env":"local"}`.
   `selection.keys` is always required, even for an empty input. To select a bundle,
   add `"bundle":"api"` inside selection and supply the complete allowed key
   ceiling. A bundle member outside that ceiling rejects the request; selection
   never silently intersects the bundle with the ceiling.
3. Save `data.item.execution_id`, `input_id`, `external_execution_id` and `revision`.
   Preparation freezes exact selected bytes and bundle membership. Later ordinary
   edits do not change them. The deadline is at most 30 seconds from preparation,
   shortened by a selected value's TTL or the original key-session deadline.
4. After the caller approves that exact job/input, call `values execution approve
   EXECUTION_ID --profile app --request-id UUID --stdin` with
   `{"revision":1,"runner_id":"RUNNER_ID","runner_instance_id":"INSTANCE_ID","external_execution_id":"job-42","input_id":"INPUT_ID"}`.
   Substitute returned IDs and revision; uppercase placeholder text is illustrative.
5. Call `values execution deliver EXECUTION_ID --profile app --request-id UUID
   --stdin` with `{"revision":2}`, using the approval's actual returned revision.
   Delivery verifies the original session, lock/protector identity, storage
   generation, expiry, approval and registered instance immediately before the
   durable send intent. It then initiates at most one sensitive send.

`values execution cancel EXECUTION_ID` uses the same request/body options and
`{"revision":REVISION}`. It removes pending delivery authority and snapshot bytes
before an attempt. It does not cancel an external job. Once an attempt exists,
cancellation cannot authorize a replacement send.

The receiver must authenticate its peer through the OS user identity and enforce
protocol 1 with fresh challenges and the exact root/vault/runner/instance/input
binding. Unix uses private local sockets; Windows uses private local named pipes
with SID checks and remote clients rejected. Arbitrary endpoint URLs and delegation
to another service account are outside this version. WSL and native Windows use
separate data roots and identity boundaries.

### Keep identifiers and revisions separate

| Field | Meaning and use |
| --- | --- |
| Mutation `--revision` | Opaque string for the whole profile, returned by `values revision`; use only for value, env and bundle edits. |
| Execution body `revision` | Positive integer for one execution record, returned in `data.item.revision`; use the latest value for a new approve, cancel, deliver or report operation. |
| `--request-id` | Caller-owned UUID for one operation and its exact input; preserve it for retries. Execution operations share a profile's request namespace; runner operations use a separate root-wide namespace. |
| `execution_id`, `input_id` | Two distinct 32-character lowercase hexadecimal IDs returned by prepare. They are not request UUIDs or local process IDs. |
| `runner_id`, `instance_id` | Canonical lowercase UUIDs for a logical caller and one receiver instance. A successor keeps the runner ID and gets a new instance ID. |
| `external_execution_id`, `external_ref` | Caller job identity and stable engine reference. Keep them free of secrets; reports and status expose them as metadata. |

For an identical retry, preserve the original body revision even if status now
shows a newer revision. Read current status and use a fresh UUID when making a
new control decision. A `values revision` token cannot replace the execution's
integer revision.

## Recover an uncertain delivery or execution

Recover a lost prepare response using `values execution status --profile app
--request-id PREPARE_UUID`. For current state use `values execution status
EXECUTION_ID --profile app`, or `values execution list --profile app --limit 50`.
List pages use revision-bound cursors; restart pagination after `cursor_invalid`.
Status, list and reports never submit an external job.
The prepare UUID selects the execution's **current** status, not an immutable
copy of the prepare response. Repeating prepare itself returns its saved result
without refreshing the input or deadline. A later unlock also does not renew
the original input's session authority.

Use the observed error to choose recovery:

| Observation | Next action |
| --- | --- |
| Lost mutation response | Retry the saved mutation input, revision and UUID; `values request show` inspects the saved result without executing it. |
| Lost prepare response | Resolve the original prepare UUID with execution status; retain the returned execution/input IDs. |
| `revision_conflict` | Inspect current state before a new decision and use a fresh UUID; do not rewrite a saved retry body. |
| `request_conflict` | Find the original operation/input for that UUID. Give a genuinely new operation a fresh UUID. |
| `request_expired` | The old UUID stays spent. Inspect current state and the original engine job; a new UUID is not evidence that repeating an external effect is safe. |
| `execution_input_expired` or `vault_locked` | Inspect execution status and unlock explicitly when needed. Pending inputs may be canceled; any replacement needs a new prepare and approval after the caller checks that no prior attempt/job would be duplicated. |
| `delivery_pending` | Inspect current status and query the original engine job. The active sender still owns the attempt. |
| `delivery_outcome_unknown` or `delivery_already_started` | Query the engine by the original job/reference, then report verified facts. Never create a replacement delivery just to obtain a successful response. |

An absent engine result is not proof that the job never started. Keep the outcome
unknown when the engine cannot provide a trustworthy lookup.

Delivery state and execution state are separate. `delivered` means the receiver
acknowledged acceptance and installed log protection; it does not mean started.
A sender crash, partial send or missing ACK after durable intent yields
`delivery_outcome_unknown`. An active attempt can return `delivery_pending`.
Retrying the original UUID or supplying a new one cannot resend that input. A free
OS operation lease can establish that a send is abandoned, but cannot establish
whether the engine ran. Do not infer completion from a PID or timeout.

The caller must query its engine using the original `external_execution_id` or
stable engine reference. Report verified facts with `values execution report
EXECUTION_ID --profile app --request-id UUID --stdin` and fields `revision`,
`runner_id`, `external_execution_id`, `input_id`, `state`. `started` requires a
stable `external_ref`; `finished` also requires `exit_code`. `failed` and `unknown`
must repeat an already recorded `external_ref` when one exists; omitting or
changing it conflicts with the saved execution facts. These two states
require a fixed `reason`: `runner_start_failed`, `runner_lost`, `execution_failed`,
`canceled` or `outcome_unavailable`. Raw engine exceptions are not accepted.

Reports save `fact_source: runner_reported`. A registered successor of the same
logical runner may reconcile a matching job/input; its reporter instance is saved
separately without rebinding the original recipient. Authoritative started/finished
facts can resolve unknown delivery. An abandoned in-progress delivery receipt is
finalized atomically with the report. Already completed request results remain
unchanged, so replay may still return an old unknown result while status shows the
reconciled state. Finished/failed outcomes are terminal; conflicting facts and
regressions fail with `execution_state_conflict`.
In particular, a reported start cannot regress to `unknown`; a finished or failed
report cannot be replaced by a different terminal outcome.

## Run the reference caller

From this checkout, build the receiver:

```sh
go build -o /ABS/PATH/value-handoff-runner ./examples/value-handoff-runner
```

Replace the absolute paths, canonical runner/instance UUIDs and fixed helper
arguments below with your own. Choose a canonical, caller-owned private ledger
directory outside the devtools data root and workspace. Initialize it once,
then run the receiver. These examples use POSIX shell line continuation; on
Windows use the built `.exe` path and put each command on one line.

```sh
/ABS/PATH/value-handoff-runner --ledger /PRIVATE/LEDGER --init-ledger
/ABS/PATH/value-handoff-runner --root /PRIVATE/DEVTOOLS_DATA \
  --runner-id RUNNER_ID --instance-id INSTANCE_ID \
  --ledger /PRIVATE/LEDGER --program /ABS/HELPER -- ARG...
```

These are metadata and fixed helper arguments. Selected
values cross only the dedicated child environment boundary, never argv, workspace
files or ordinary JSON responses. The example does not inherit the parent env.

The example reserves the external job ID in a private metadata-only ledger before
ACK. One process-local claim may start the helper once. Lookup with `--ledger
/PRIVATE/LEDGER --lookup-job job-42` returns durable metadata without starting work.
A reservation left by a crash stays unknown. Missing history fails closed; never
reinitialize a lost ledger to recover an old job. Keep the ledger for as long as
job IDs can be retried. The example is conservative about progress and makes no
universal exactly-once guarantee. A production engine can provide stronger lookup
and lifecycle control behind the same contract. Invoke the devtools report command
separately with verified metadata; the receiver receives no vault master key.

The lookup JSON uses `external_execution_id`, `input_id`, `external_ref`, `state`
and, for a completed helper, `exit_code`. Add the latest devtools record revision
and logical `runner_id` to build the report body. A lookup's `unknown` state also
needs one of the fixed report reasons; do not send the lookup JSON unchanged.

## Logs, retention and observation

Private protocol 1 carries selected values and raw nonempty secret patterns only
over the authenticated private channel. The receiver installs the existing
literal-byte and Go JSON-string-body matcher before acceptance or first output.
Both streams remain protected until EOF, including after lock or a stored value
change. Short secrets are included. Limits fail closed: 4,096 normalized patterns,
64 KiB per pattern, 4 MiB normalized pattern bytes and the existing matcher state
limits. Control frames cap at 128 KiB; encoded payloads at 16 MiB. No raw fallback,
generic pattern query or provider-specific stdout masking command exists.

This protection does not cover arbitrary encodings, application files/network,
new tokens produced by the application or data already logged upstream. A private
first hop does not make a Docker environment or an existing CI logging path safe.
OS identity cannot prove that a custom receiver installed its declared protection.

Execution snapshots and control records are encrypted and authenticated with root,
profile and path binding. Successful delivery, cancellation and reports atomically
remove the sender snapshot. Expired pending snapshots are retired by authenticated
mutation/replay of that execution. Reads do not erase them; cleanup can wait while
locked. TTL is an admission deadline, not immediate physical or forensic erasure.
An attempted or delivered input never becomes expired work that may be resent.
Execution/request metadata remains for the data-root lifetime; completed replay
uses the original 30-day request window and expired IDs never become new work.

`diagnostics.external_executions` exposes only verified aggregate counts, or
`verification_pending` without counts when verification is unavailable. It never
unlocks, renews idle or returns job IDs, patterns or snapshot contents. Existing
`values uses` and process status remain tied to actual local process ownership;
external records have no synthetic PID/readiness/restart behavior. Dashboard has
no execution-control endpoint in this version.

Portable profile transfer and encrypted logical backups include values and task
data, excluding these runtime registrations, approvals, snapshots and transport
sessions. Restore/rotation invalidates pending original generation authority.
Unknown external outcomes remain separate from devtools' own launch journal and
do not indefinitely block lock or restore. Process-kill tests establish crash
recovery behavior; they do not simulate sudden physical power loss.
