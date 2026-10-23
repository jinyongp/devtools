# Configure and execute project commands

## Choose the operation

| Need | Use | Check |
| --- | --- | --- |
| Inspect declarations | `config inspect`, `command list/inspect/explain` | Selected project and effective fields |
| Edit setup | `command add/update`, `config tool/preset/edit` | Revision, preview, `config check` |
| Diagnose prerequisites | `doctor COMMAND` | `data.ready`, checks and remedies |
| Run once in foreground | `command run NAME` (`run` alias) | Child exit code |
| Keep a server alive | `process start NAME` | Execution ID, status, readiness |
| Start several servers | `project up NAME...` | Per-command outcomes and readiness |
| Diagnose the installation | `diagnostics` | Subsystem results; stopped is not necessarily broken |

- Reuse configured commands before invoking underlying package scripts directly.
- Ports and URLs: [ports.md](ports.md). Values/injection: [values.md](values.md).
- Proxy routes: [network.md](network.md). Protection errors: [protection.md](protection.md).

On Linux/macOS, `env list`, `variable list/get` (`var`) and `secret list` (`sec`)
can query a clean, initialized store under read-only sandbox access. They still
take the ordinary maintenance locks. Missing locks or pending recovery can require
writes: inspect `facts.state` (`initialization_required` / `recovery_required`) and
the `value_write` remedy, run the scoped query in an authorized writable context,
then retry. Do not change permissions, remove locks, or elevate automatically.
Read denial is a separate storage failure. Windows and other query commands retain
their existing access requirements; metadata queries do not unlock or renew a vault.

## Project configuration

- Profile resolution: explicit `--profile`, then nearest `devtools.toml`; lookup stops at the Git root. Remote URLs do not imply a profile.
- `init --profile NAME` connects a new project without overwriting existing configuration.
- Discover `schema config edit` before using typed editing on a supporting build.
- Prefer `command add/update/remove/rename`, `config tool/preset`, or atomic `config edit` over reconstructing TOML.
- `config inspect` returns the revision. Updates/removals/renames/batches require it; adds are create-only.
- Preview with `--dry-run`; apply unchanged input with the same revision. Re-inspect conflicts; do not bypass source/permission checks.
- `config check` validates declarations and static references. `doctor` checks actual prerequisites. `command explain` shows effective fields/provenance without running tools or reading stored values.
- On protocol 10, bare `doctor` checks all configured commands; `--project-only` checks defaults, `--mode managed` includes launcher/readiness. Inspect common and per-command checks; unknown is unverified.
- `doctor --check` exits 3 when not ready, with the full report in `error.details.report`. Missing argv[0] exits 127; other prerequisite failures exit 3. Retain dir/profile/env/bundle/mode when following remedies.
- Diagnosis is passive and does not unlock protection. `diagnostics.data.ready: false` means a subsystem could not be inspected safely, not simply that a proxy or backup is inactive.
- Tools register specifications; presets reuse selections. Only defaults apply globally. Do not promote command requirements to defaults or install tools implicitly.
- Legacy requirements cannot mix with registry fields. `config migrate` needs an explicit preview, revision and apply digest; `config fmt` only formats.

### Tool versions

- Discover npm range support through `config schema`: `$defs.Version.x-npmRange` is true on supporting builds. Older 0.30.1 builds compare exact strings.
- `26` selects a major family; `26.12` a minor family; `26.12.3` an exact version. Caret, tilde, comparisons, hyphen ranges and `||` follow npm SemVer, including prerelease and 0.x rules.
- Omission checks presence only; `*` probes any stable version. Requirements are nonempty, control-free, at most 128 bytes.
- Two-component observed versions compare with patch zero. Multiple requirements for the same executable/probe must intersect, including resolved file aliases.
- Preserve an exact pin using a verified full version; never blindly append `.0`. Migration converts structure, not the old exact meaning of `26.12`.
- Installation/version switching belongs to the project's existing tool manager.

## Foreground execution

- Named commands run from the configuration directory; direct `run -- PROGRAM ARG...` runs from the caller's directory.
- `exec` is an argv array. Shell syntax needs an explicit shell; port arguments use `${bind.NAME}`, not a shell wrapper.
- From v0.23.0/protocol 6, put devtools flags **before** the command name. Every later token is a child argument, including `--help` and `--env`.
- `devtools run --help` is CLI help; `devtools run NAME --help` is child help.
- `devtools run --env local NAME -- --port 3000` also works on older protocol 5 builds. One leading child `--` is a separator; later ones pass through.
- Since v0.22.2, command keys support punctuation, spaces and Unicode (1–128 characters), but not leading `-` or controls. Quote TOML keys and shell arguments as needed.
- Named commands default to `inject = false`; explicit `--env` enables injection. Inspect selection before overriding it; see [values.md](values.md).

## Managed servers and readiness

1. Start the selected command with `process start` or named servers with `project up`; supply a request UUID.
2. Save each returned execution ID (`data.item.id` for a single start).
3. Inspect status. If readiness is declared, use `process wait EXECUTION_ID` before dependent work.
4. Use `process` operations for an execution ID; `project` operations for selected commands in the current project's canonical directory.

- `running` describes lifetime. Port occupancy or successful start does not prove readiness.
- Declare `commands.NAME.ready.exec` as a repeat-safe check returning exit 0 when ready. `ready.timeout` limits each probe.
- The probe uses the execution's frozen directory/environment/bindings. `process check` may exit 0 with `readiness.ready: false`; `process wait` succeeds only on readiness.
- Missing readiness: add the declaration and restart if the user needs readiness verification. Do not report an undeclared check as passed.
- Restart applies current config/values. It does not start a missing command implicitly.
- `project status/logs/restart/down` select this project, not other worktrees sharing its profile. Names are required for up/restart; do not assume every configured command is a server.
- `project logs NAME` reads active captured output; execution-ID `process logs` reads ended history. Logs are discarded unless capture was requested.
- Since protocol 12, run/start/up can prepare matching proxy routes automatically. See [network.md](network.md); older builds need `--proxy`. There is no TOML `proxy = true` field.
- For a named command, omitting `--env` inherits its configured env. An explicit
  `--env ''` selects common values; a direct executable uses common values by
  default. Place run options before the command name or child `--` boundary.
- Recovery argv for the same store retains the selected isolation root. Inspect
  `retry`, `effects`, and `required_inputs` before acting; passphrase unlock needs
  private stdin/file, while native unlock may need explicit OS authentication.

## Restart and retry

- Restart/down freeze original execution targets. Retry identical input/UUID to continue unfinished children; successful siblings remain running.
- On protocol 10 partial failures, inspect `error.details.facts.outcomes` and `causes`; older builds use `error.details.items`.
- Batch `replayed: true` can include updated progress. Completed requests replay saved results; query current state separately.
- `process_pending`: retry the same request. Supervisor loss: inspect state; relaunch requires a new, authorized start/restart, not replay of the old start.
- Automatic value updates and paused replacement chains: [values.md](values.md).
