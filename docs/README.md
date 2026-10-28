# devtools documentation

Start with the [root README](../README.md) for installation and a first project.
A [Korean README](../README.ko.md) is also available. Check examples against your
installed CLI's help and schema. Installers select the latest stable release;
this checkout also documents source features that may not yet be published.

## Check feature availability

`devtools version` identifies the executable you actually run. A protocol number alone
does not establish vault support: discover each feature with `devtools schema`,
for example `devtools schema vault init`, `devtools schema bundle set`, and
`devtools schema command run`. A release or Skill update does not update an
already-installed CLI, migrate old secret files, or restart existing services.

For guarded mutations and approved same-user runner delivery, see
[secret automation](secret-automation.md). Check command discovery before using
an older installed release.

For release preparation and publication checks, use the
[development guide](development.md#릴리스). For existing secret storage, follow the
[migration procedure](secret-protection.md#기존-저장소를-전환하기).

<a id="문서-버전과-예제-읽기"></a>

## Reading examples

Use your installed CLI to look up its commands and machine-readable contracts:

```sh
devtools version
devtools --help
devtools schema command run
```

- **Shell:** `sh` blocks use POSIX syntax. PowerShell uses `$env:NAME` and its own
  quoting rules. Programs invoked by examples, such as `sh`, Python, or pnpm, must
  be installed separately. The [Windows guide](windows.md) covers native execution.
- **Paths:** replace example paths with local directories. Quote paths containing
  spaces. For Windows paths in TOML, use literal strings such as `'C:\tools\app.exe'`
  or escape backslashes.
- **Files:** prepare secret, passphrase, or identity files before using them.
  PowerShell 5.1 text pipelines can change encoding and newlines; use `--file` when
  exact secret bytes matter.
- **Request IDs:** replace `UUID` with a new UUID for each mutation. Use `uuidgen`
  in a POSIX shell or `[guid]::NewGuid().ToString()` in PowerShell. After an uncertain
  response, retry with the original ID and identical input. Take `TASK_ID`, `RUN_ID`,
  `EXECUTION_ID`, and `DIGEST` from earlier responses.
- **Output:** data commands return JSON. `ok: true` does not imply a diagnostic's
  `ready: true`. `run` keeps child streams outside JSON and preserves the exit code, with [output protection](output-protection.md) for new executions.

Some linked topic guides are currently in Korean.

## Installation and platforms

- [Installation and updates](install.md): latest stable installers, custom paths, and offline assets.
- [Windows](windows.md): native installation, PowerShell, permissions, and validation limits.
- [WSL](wsl.md): Linux installation, independent storage, Windows browser access and CA trust.
- [Shell completion](completion.md): bash, zsh, fish, and PowerShell setup.
- [Agent Skill](agent-skill.md): install, update, and remove agent guidance separately from the CLI.

## Working in a project

- [Configuration editing](configuration.md) (Korean): tools, presets, typed changes, revision guards, migration, and formatting.

- [Values and commands](cli-contract.md): profiles, environment overrides, variables, secrets, dotenv import, and saved commands.
- [Local secret protection](secret-protection.md): one-time vault setup, reusable sessions, locking, migration, and recovery.
- [Reusable value bundles and usage](cli-contract.md#재사용할-값-묶음-bundle): save var/sec selection once and inspect delivery without reading secrets.
- [Diagnostics](doctor.md): `doctor`, `diagnostics`, required tools, and recovery.
- [Ports](ports.md): assignments, bindings, project URLs, and worktree names.
- [Processes](processes.md): start, logs, restart, stop, and partial batch failures.
- [Process operation locks](process-operation-locks.md): request/singleton ownership, maintenance barriers and crash recovery.
- [Operation reliability review](operation-reliability-review.md): protocol 19 scope, review coverage and final verification requirements.
- [Readiness](process-readiness.md): wait until a server can accept requests.
- [Local networking and sharing](local-network.md): domains, HTTPS, listeners, external sharing, and cleanup.
- [Development server compatibility](proxy-compatibility.md): Host policy, framework allowlists, real RPC/HMR and recovery.
- [Legacy proxy guide](proxy.md): links to the current network guide.
- [Tasks and workstreams](tasks.md) (English): plans, claims, validation, and handoffs.
- [Dashboard](dashboard.md) (English): sign in, edit plans and values, and manage resources in a browser.
- [Profiles](profiles.md): metadata, encrypted transfer, preview, and replacement.
- [Backups and recovery](backup.md): keys, encryption, preview/apply, and interrupted restores.
- [Storage cleanup](cleanup.md): selection, archive recovery, and permanent deletion.
- [F01 follow-up evidence](f01-followup-results.md) (Korean): public protection changes, native checks, and measured CLI startup improvements.

## Automation and API contracts

Use `devtools schema COMMAND` for the installed CLI's input and output contracts.
Design background is separate from public command availability.

- [Execution output protection](output-protection.md): per-stream modes, TTY/raw exceptions, capture masking, and limits.
- [CLI output](cli-output.md): output modes, JSON paths, errors, and protocol migration.
- [Task API](task-api-contract.md), [plan editing](task-edit-api.md): state transitions, revisions, and retries.
- [Dashboard API](management-api-contract.md): authentication, Origin, mutations, and previews.
- [Port contract](port-design.md), [network contract](network-design.md) (English): identity, declarations, addresses, and ownership.
- [HTTP proxy contract](proxy-design.md): route selection, forwarding, redirects, and connection lifetime.
- [Local var/sec contract](var-sec-contract.md): authenticated storage, key sessions, launch snapshots, and lock guarantees.
- [Runner receiver protocol](runner-receiver-protocol.md): Unix endpoint ownership, private framing, identity checks, and uncertain delivery recovery.

## Development and verification

- [Development](development.md): checkout setup, checks, dashboard changes, packaging, and releases.
- [Verification layout](verification.md) (Korean): recurring CI, local checks, diagnostics, and maintenance rationale.
- [Installed-release verification](../verify/README.md): Unix scenarios and native Windows Actions.
- [Network coverage](../verify/network-coverage.md): checks and unverified environments.
- [Plan-editing regression verification](task-edit-verification.md): coverage and isolated test commands.
- [Diagnostic contract follow-up review](cli-diagnostic-contract-review.md) (Korean): protocol 17 scope, review coverage, and native validation requirements.
- [Performance](../perf/README.md) (English): Linux/WSL measurements and baselines.
- [Agent Skill source](../skills/devtools/SKILL.md): operational guidance read by agents.

## Architecture

- [Task and workstream design](task-workstream-design.md): shared state, execution contexts, and completion rules.
- [Storage and recovery](storage-design.md): private files, locking, journals, and recoverable replacement.

## Implementation contracts

These contracts describe source behavior through protocol 19. Check the installed binary's
`version` and `schema` before using a new interface. Platform acceptance requires
native execution; cross compilation alone does not establish publication or ACL behavior.

- [Project configuration and CLI diagnostics](cli-config-redesign.md) (Korean): tool definitions and presets, compatibility, migration, and design rationale.
- [Configuration editing](config-edit-contract.md) (Korean): command inputs, revision guards, responses, publication, and recovery.
- [Diagnostics and doctor](diagnostic-contract.md) (Korean): public errors, scoped preflight checks, passive authentication, and acceptance criteria.
- [Contract schema](../internal/configcontract/schema.json): machine-readable configuration, mutation, response, and diagnostic definitions.
- [Conformance cases](../internal/configcontract/cases.json): synthetic valid/invalid schema and strict JSON transport examples.
- [Diagnostic requirements](cli-guidance-audit.md) (Korean): error context, recovery guidance, and acceptance criteria across CLI domains.
