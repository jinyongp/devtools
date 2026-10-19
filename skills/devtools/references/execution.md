# Project setup, commands and managed servers

## Project and values

Inspect existing configuration before editing it. For a new connection use
`init --profile NAME`: it creates devtools.toml without overwriting an existing
configuration. Profile selection is explicit --profile, then nearest devtools.toml;
lookup checks the Git root and stops there. No profile is inferred from a remote.
Keep project declarations tracked and secret values in the user data store.

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
Use `diagnostics` when the question spans the user's devtools installation instead of one
project command. It reports profile, port, process, proxy, dashboard, and public backup
metadata without secret values, raw logs, credentials, execution contexts, or backup
recipient material. A stopped proxy/dashboard or unconfigured backup is a readable state;
`diagnostics.data.ready: false` means at least one subsystem could not be inspected safely.
Each remedy has `argv`, `required_inputs`, and `message`. Supply missing inputs before
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
`devtools run --env local dev:docs -- --port 3000`. This form also works on v6/v7.
For CLI protocol v6/v7 (v6 begins at devtools v0.23.0), put all devtools options before the configured command name:
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

## Managed server lifecycle

Process start/stop/restart require request UUIDs. Save the returned execution ID
from data.item.id. `process status` reports lifetime; a configured
`process wait EXECUTION_ID` establishes readiness before dependent work. `process check`
can exit successfully with `readiness.ready: false`.
Restart applies current config and values.

Use `project up COMMAND... --request-id UUID` for several named servers. Names are
required; never assume all configured commands are servers. Readiness waits are
per command. `project status [COMMAND...]`, `project logs COMMAND`, `project restart
COMMAND... --request-id UUID`, and `project down [COMMAND...] --request-id UUID` select
the current project's canonical directory, not other worktrees sharing its profile.
`project logs` reads the active captured output for that command; use execution-ID
`process logs` for ended history. Restart freezes the active execution IDs selected by
the first request, applies current project configuration and values, and never starts a
missing command implicitly. Omitted env/capture options inherit the execution's prior
selection. On partial failure inspect `error.details.items`; successful siblings stay
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
