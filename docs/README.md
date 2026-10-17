# devtools documentation

Start with the [root README](../README.md) for installation and a first project.
A [Korean README](../README.ko.md) is also available. These guides assume the latest
stable release; release-specific changes belong in release notes and historical records.

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
  `ready: true`. `run` preserves the child's output and exit code.

Some linked topic guides are currently in Korean.

## Installation and platforms

- [Installation and updates](install.md): latest stable installers, custom paths, and offline assets.
- [Windows](windows.md): native installation, PowerShell, permissions, and validation limits.
- [WSL](wsl.md): Linux installation, independent storage, Windows browser access and CA trust.
- [Shell completion](completion.md): bash, zsh, fish, and PowerShell setup.
- [Agent Skill](agent-skill.md): install, update, and remove agent guidance separately from the CLI.

## Working in a project

- [Values and commands](cli-contract.md): profiles, environment overrides, variables, secrets, dotenv import, and saved commands.
- [Diagnostics](doctor.md): `doctor`, `diagnostics`, required tools, and recovery.
- [Ports](ports.md): assignments, bindings, project URLs, and worktree names.
- [Processes](processes.md): start, logs, restart, stop, and partial batch failures.
- [Readiness](process-readiness.md): wait until a server can accept requests.
- [Local networking and sharing](local-network.md): domains, HTTPS, listeners, external sharing, and cleanup.
- [Legacy proxy guide](proxy.md): links to the current network guide.
- [Tasks, workstreams, and dashboard](tasks.md) (English): plans, claims, validation, handoffs, and browser management.
- [Profiles](profiles.md): metadata, encrypted transfer, preview, and replacement.
- [Backups and recovery](backup.md): keys, encryption, preview/apply, and interrupted restores.
- [Storage cleanup](cleanup.md): selection, archive recovery, and permanent deletion.

## Automation and API contracts

Use `devtools schema COMMAND` for the installed CLI's input and output contracts.
Design background is separate from public command availability.

- [CLI output](cli-output.md): output modes, JSON paths, errors, and protocol migration.
- [Task API](task-api-contract.md), [plan editing](task-edit-api.md): state transitions, revisions, and retries.
- [Dashboard API](management-api-contract.md): authentication, Origin, mutations, and previews.
- [Port contract](port-design.md), [network contract](network-design.md) (English): identity, declarations, addresses, and ownership.
- [Legacy HTTP proxy contract](proxy-design.md): HTTP compatibility; extensions follow the network contract.

## Development and verification

- [Development](development.md): checkout setup, checks, dashboard changes, packaging, and releases.
- [Installed-release verification](../verify/README.md): Unix scenarios and native Windows Actions.
- [Network coverage](../verify/network-coverage.md): checks and unverified environments.
- [Plan-editing regression verification](task-edit-verification.md): E01–E49 and historical criteria.
- [Performance](../perf/README.md) (English): Linux/WSL measurements and baselines.
- [Agent Skill source](../skills/devtools/SKILL.md): operational guidance read by agents.

## Design and historical records

These records preserve decisions and evidence for their stated source and date.
Use the guides above for current installation and behavior.

- [Documentation review](documentation-review.md).
- [Windows release preparation](releases/v0.25.0.md).
- [Task/workstream design](task-workstream-design.md), [management roadmap](management-roadmap.md).
- [Proxy MVP](proxy-plan.md), [profile/project lifecycle](profile-project-lifecycle-plan.md).
- [Hardening](devtools-hardening-plan.md), [storage correctness](correctness-recovery-storage-plan.md), [process/cleanup recovery](process-lifecycle-recovery-plan.md).
- [Profile retirement proposal](profile-retirement-contract.md): a proposal, not an installed feature list.
- [Agent Skill installation plan](agent-skill-install-plan.md), [installer Pages plan](pages-installer-plan.md).
- [Windows plan](windows-support-plan.md), [research](windows-support/research.md), [validation policy](windows-support/implementation/validation-policy.md), [final implementation review](windows-support/implementation/W9.md), [completion record](windows-support/implementation/completion.json).
