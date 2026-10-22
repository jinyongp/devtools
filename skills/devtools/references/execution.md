# Project setup, commands and managed servers

## Project and values

Inspect existing configuration before editing it. For a new connection use
`init --profile NAME`: it creates devtools.toml without overwriting an existing
configuration. Profile selection is explicit --profile, then nearest devtools.toml;
lookup checks the Git root and stops there. No profile is inferred from a remote.
Keep project declarations tracked and secret values in the user data store.

On protocol 10 builds, discover `schema config edit` before editing. Prefer typed
`command add/update/remove/rename`, `config tool`, `config preset`, and atomic
`config edit` over reconstructing TOML. `config inspect` returns the file revision;
updates, removals, renames and batches require it. Preview with --dry-run, retain
the same revision for apply, and re-inspect on conflict. Adds are create-only.
Tools register specifications; presets reuse selections; only defaults affect all
commands. Do not infer dependencies, install tools, or promote selections globally.
Legacy requirements remain readable but cannot be mixed with registry fields.
Use `config migrate` preview and its revision/apply_digest for explicit conversion.
`config fmt` only formats. `command explain` shows effective fields and provenance
without running tools or reading stored values. Preservation failures require a
reviewed edit; do not bypass the editor's source or permission checks.

Discover range support from `config schema`: `$defs.Version` declares
`x-npmRange: true` on supporting builds. Older 0.30.1 builds compare exact version
strings. Supported requirements follow npm SemVer: `26`/`26.x`/`26.*` select a
major family, `26.12`/`26.12.*` select a minor family, and `26.12.3` is exact.
Caret, tilde, comparisons joined by spaces, hyphen ranges and `||` use npm's
prerelease and 0.x rules; build metadata does not affect precedence. Empty or
control-containing requirements are invalid; `*` probes any stable version,
while omission only checks executable presence. Conditions are at most 128 bytes.
Two-component observed output is compared with patch zero; diagnostics keep the
raw token. Multiple requirements for the same executable and probe argv must
have a common version across the complete set, including resolved file aliases.
Installing and switching tools remains the external version manager's job.
The old exact meaning of `26.12` is intentionally replaced. For exact pins,
inspect the actual release and choose a full version; do not blindly append `.0`.
`config migrate` converts structure, not the old version meaning. Do not add a
legacy flag or a top-level config version to preserve the previous interpretation.

Create an env with `env create NAME` before selecting it. Without --env, value
operations address common scope; with --env, reads merge common and env overrides
while writes/unsets affect only that env. Removing an override reveals the common
value. A key's variable/secret kind is profile-wide and persists after values are
unset. Remove directly stored env values before removing the env.

Use `variable`/`var` for public values and `secret`/`sec` for metadata-only secret
management. Secret set accepts exactly one of --file or --stdin and preserves
input bytes, including a trailing newline. Do not read secret files into command
arguments. Preview dotenv import with --dry-run; existing key kinds are retained,
new keys named by --var are public, and other new keys become secrets. Different
stored values require explicit --overwrite; failed import preserves all values.

Discover `schema values revision` before using opt-in automation guards. On supported
builds, set/unset, env create/remove and import accept --revision and --request-id
only as a pair. Read the edit token with `values revision`; preserve the UUID,
revision and exact input for response-loss retries. Same ID/different input conflicts;
successful no-ops also bind IDs. Guardless calls preserve their old contract but
cannot express a caller-observed stale editing intent. Guarded set additionally accepts
--create-only: an empty/expired exact slot exists, while inherited common is not an
env slot. Guarded import preserves CLI layer comparison with 500,000-byte/5,000-key limits.
Preview binds no receipt. Do not put secret values in argv or workspace files.

`values request show UUID` returns saved metadata using only an existing warm key,
without starting/unlocking a daemon or extending idle. Missing means request_not_found,
not permission to guess success or issue a different request. Successful retries replay
for 30 days; expired IDs return request_expired and stay spent. Metadata remains for the
data root's lifetime. Timestamp-free legacy receipts remain unlimited. Root deletion,
historical full rollback and transfer to another root end this local deduplication scope.

`secret generate KEY --revision REVISION --request-id UUID` stores 32 random bytes
as 43 unpadded base64url characters and returns metadata only. Protection must already
be initialized/unlocked. It creates an exact-layer slot by default; --replace permits
upsert while preserving a live slot's expiry and rejecting an expired slot. It never
changes a variable's kind. Retry identical committed requests without regenerating;
there are no length/encoding/seed options or plaintext output.

## Local key protection (discover installed support first)

Check `schema vault status` and `schema vault init`; older installed builds may not
provide them. On supporting builds, public-only roots need no vault. Before the first
secret, use explicit one-time `vault init` with existing native protection or explicitly
choose `--backend passphrase` for a keyringless host. Do not install/start a keyring or
silently change protection backend. Isolated roots require passphrase protection.
`vault status` is passive. A successful key session is reused across commands and profiles;
never put a passphrase in argv/env. Use explicit private-file/piped inputs or hidden
terminal input for init/unlock. Native authentication UI requires `--interactive`.
Manual lock survives daemon restart; passphrase sessions require explicit unlock after
idle expiry. Ordinary metadata queries, completion and diagnostics do not unlock or
extend the session. Protected-root writes/execution require authentication even for a
public profile; no-injection commands keep their existing behavior.

`vault recover` finishes an interrupted storage transition after explicit unlock; it
verifies existing protection rather than inventing a replacement key. Do not remove
key/catalog/recovery files or infer execution completion from a PID. Unknown admission
stays unresolved until trustworthy recovery evidence is available. Lock cannot revoke
values already delivered to applications. Original dotenv files remain after import.

## Reusable value selection and usage

Profile/env remains the default whole-profile selection. Discover `schema bundle set`
before using optional named bundles on a supporting build. A bundle selects shared
var/sec ValueIDs; it does not copy values or encode an env. Register membership once,
then select it on later runs rather than enumerating keys for every command.
Use the revision from `bundle list` and a request UUID for atomic bundle edits.
`import --bundle NAME` adds imported keys to that reusable selection in the same
transaction; keep the import's existing kind and overwrite rules.

Selection precedence is explicit CLI `--bundle`, command `bundle`, project `bundle`,
then `all`. `none` selects no profile values; explicit `--env` does not undo it.
An unknown bundle fails instead of falling back to all. Bindings must be inside
the selected bundle. Parent environment inheritance is a separate policy: none
does not remove a credential already in the parent. Bundles are not an app sandbox.
Renaming preserves a bundle ID; removing membership does not delete the value.
Changes affect later executions, including a new member added after registration.

Use `values inspect` for selected slot/version/incarnation metadata and `values uses`
for the current project's declared selections versus actual recorded deliveries.
These reports contain no values and do not observe application reads or external
authentication. `command inspect` reports configured selection; a running execution
can retain a different explicit override. Query its execution ID before acting.
Passive inspection neither unlocks nor extends the key session. Locked protection
reports verification_pending; an old execution without delivery metadata is unknown.
Public-only profile bodies remain integrity_verified:false, even in a protected root.

Changed effective input reports pending; the default policy is notification only.
Unselected values, hidden common overrides and same-value writes do not change a
delivery. Use an explicit managed restart when the user wants to apply new input;
old applications keep their original environment and output patterns until exit.

## Diagnose preparation

Use `doctor COMMAND` before a configured command when setup is uncertain.
A successful diagnosis can still have `data.ready: false`; inspect `checks[].remedies`.
On protocol 10, bare doctor checks all configured commands. Inspect common checks
and `commands[].checks`; unknown is unverified, not pass. `--project-only` checks
defaults, `--mode managed` includes launcher/readiness, and `--check` returns exit 3
with the full report in `error.details.report` for non-ready results. Diagnosis is
passive and does not unlock the vault. Retain dir/profile/env/bundle/mode when
following a scoped remedy. Missing argv[0] is executable_not_found (127), while
other prerequisite failures are requirements_failed (3); inspect all checks/causes.
Use `diagnostics` when the question spans the user's devtools installation instead of one
project command. It reports profile, port, process, proxy, dashboard, and public backup
metadata without secret values, raw logs, credentials, execution contexts, or backup
recipient material. A stopped proxy/dashboard or unconfigured backup is a readable state;
`diagnostics.data.ready: false` means at least one subsystem could not be inspected safely.
Each remedy has `argv`, `required_inputs`, `message`, and (protocol 10) `effect`. Supply missing inputs before
execution; an empty argv describes a manual action. These are suggestions, not automatic
permission to install tools or change data. Keep secrets in stdin/files, never remedy argv.
Secrets enter processes through `command run` or managed commands. On supporting builds,
new non-TTY stdout/stderr and captured logs mask delivered secrets. TTY/raw exceptions,
historical logs, application files and unknown/encoded tokens remain outside that
guarantee; never treat output filtering as a sandbox.

## Foreground execution

Use `command list` to discover configured commands, `command inspect NAME` to
inspect one, and `command run NAME` for foreground work. Use `process start NAME`
for a persistent server.
In devtools 0.22.2 and newer, command names are literal keys of 1–128 characters.
Punctuation, spaces, and Unicode are supported; leading `-` and control characters
are invalid. For example, `[commands."dev:docs"]` with `exec = ["pnpm", "run", "docs:dev"]`
is executed by `devtools run dev:docs`. Quote TOML keys containing punctuation and
shell arguments containing spaces or shell syntax. Profile, env, port, and proxy
names keep their separate identifier rules. Check `devtools schema command run`
for the installed command-name contract; CLI and Skill updates are separate.
On older protocol v5 builds such as v0.22.3, pass child arguments after an explicit separator:
`devtools run --env local dev:docs -- --port 3000`. This form also works on later protocols.
From devtools v0.23.0 (protocol v6), put all devtools options before the configured command name:
`devtools run --env local dev:docs --port 3000`. Every token after the name is a
child argument, including `--env`, `--profile`, `--dir`, `--help`, and `-h`.
Use `devtools run --help` for devtools help and `devtools run dev:docs --help`
for the child's help. A single `--` immediately after the name remains an optional
separator; later `--` tokens are passed through. Direct execution still uses
`devtools run --env local -- PROGRAM ARG...`. The same rules apply to `command run`
and `cmd run`; other commands keep their existing option placement.
Named commands execute from the configuration directory; direct `run -- PROGRAM`
executes from the caller's directory. `exec` is an argv array, not shell syntax;
use an explicit shell only when the project needs it. Named commands default to
`inject = false`, inheriting the parent environment. `inject = true` overlays
common profile values and the configured env; explicit --env enables injection
even when inject is false and overrides the configured env. Direct execution
injects profile values. Inspect the command before choosing an env override.

## Approved external execution

Discover `schema values execution prepare` and `schema values runner register`
before use; older installed builds do not expose these commands. See
`docs/secret-automation.md` in the devtools repository for the complete integration
contract. Registrations are root-wide; execution records select a profile.
Prepare requires explicit `selection.keys`, with the full allowed key ceiling
when a bundle is selected. Preparation freezes bytes but grants no execution
permission. Approve the exact input/job and registered runner instance explicitly.
Deliver permits at most one sensitive send. A lost ACK or sender termination
after intent remains unknown and must never trigger a replacement send/job.

Save each lowercase request UUID and exact metadata body. Recover a prepare with
`values execution status --request-id PREPARE_UUID`; inspect current execution
status separately from immutable replay results. The caller must look up its
engine by the original external job ID/reference and report verified facts.
Started/finished reports require a stable engine reference, finished also needs
an exit code. Report does not start work. A registered successor may reconcile
the same job/input but cannot rebind the original delivery recipient.

Mutation edit revisions are opaque profile-wide strings; execution control bodies
use the positive integer `item.revision` of one execution record. For an identical
retry keep the original body/revision; for a new decision inspect current status
and use a fresh UUID. Status selected by a prepare UUID returns current metadata,
while repeating prepare replays its original result without extending the deadline.
Unlock does not renew old input authority. Never replace an uncertain attempt
with a new preparation solely to obtain a successful response.

Only an authenticated same-OS-user receiver receives values and masking patterns
over the private local channel. Never route them through ordinary JSON, argv,
workspace files or provider stdout masking commands. The receiver must protect
both streams before ACK and retain old patterns through output EOF, even after
lock/value edits. Docker/remote CI adapters and other-account delegation are
outside this capability. The caller owns approval, isolation, network and cancel.
Delivery does not prove application use/authentication or process readiness.

Runtime grants/snapshots/registrations are excluded from portable backups.
Completed replay lasts 30 days; expired IDs stay spent. Status/diagnostics do not
unlock or renew idle, and locked snapshot cleanup can wait for authenticated
mutation. Never delete unknown records or recreate a missing caller engine ledger
as recovery. External records belong in execution list, not synthetic process IDs.

## Managed server lifecycle

Process start/stop/restart require request UUIDs. Save the returned execution ID
from data.item.id. `process status` reports lifetime; a configured
`process wait EXECUTION_ID` establishes readiness before dependent work. `process check`
can exit successfully with `readiness.ready: false`.
Restart applies current config and values.

For a user-selected managed server on a supporting build, command configuration
`values_update = "restart"` opts into automatic application of effective input
changes. Omitted policy is notify. Do not enable restart for a migration, deploy
job or foreground command merely because it consumes values; ended one-shot and
foreground executions are never repeated automatically.

Read process status before retrying. `value_watch` describes current input;
`value_apply` separately reports requested/applied/failed/paused and a replacement
`next_id`. Stored values, delivered input, readiness and external authentication
are distinct successes. Preparation failure keeps the old app; a failure after
old stop can leave an outage and does not roll back values or stop siblings.
An automatic attempt is not resubmitted after failure/uncertainty, even for a
later goal. Three replacements in 30 seconds also pause the chain. Resolve the
cause and inspect the current project/execution, then use an explicitly authorized
manual restart with a new UUID to start a new attempt. An unknown coordinator or
launch outcome needs trustworthy state inspection; do not invent completion from
a PID, delete coordination files, or blindly create replacement requests.

Use `project up COMMAND... --request-id UUID` for several named servers. Names are
required; never assume all configured commands are servers. Readiness waits are
per command. `project status [COMMAND...]`, `project logs COMMAND`, `project restart
COMMAND... --request-id UUID`, and `project down [COMMAND...] --request-id UUID` select
the current project's canonical directory, not other worktrees sharing its profile.
`project logs` reads the active captured output for that command; use execution-ID
`process logs` for ended history. Restart freezes the active execution IDs selected by
the first request, applies current project configuration and values, and never starts a
missing command implicitly. Omitted env/capture options inherit the execution's prior
selection. On protocol 10 partial failure inspect `error.details.facts.outcomes` and
`causes` (older builds use `error.details.items`); successful siblings stay
running. Retry the same input/UUID to continue unfinished children. A resumed batch may
return updated progress with `replayed: true`. Completed requests replay their saved
result. Down/restart retries keep the original execution targets. Use a new UUID for a
new operation and `project status` for current state.

Ports belong to execution locations. Inspect `port` and `instance` before changing
assignments. Commands declare `serve` for servers and `bind` for injected values.
Coordinate consumers when a stored port changes.

Port declarations may select a preferred port, range and strict requirement.
Execution allocates local served ports; a stored assignment alone does not prove
that a server is running or ready. Cross-instance bind references need existing
assignments. Use instance aliases to select another worktree explicitly. Release
or prune only the intended inactive assignments; do not kill an arbitrary process
from a stored PID or reclaim an occupied port without inspecting ownership.

Logs are discarded by default. Start with --capture-logs only when needed;
process/project logs can contain secrets. `process_pending` calls for retrying
the same request. An interrupted execution after supervisor loss needs an explicit
new start/restart operation; replaying its old start does not relaunch it.
