# devtools

[English](README.md) | [한국어](README.ko.md)

devtools is a CLI for managing your project's development environment and tasks.
Store environment variables and secrets, pass them to commands, and manage
development servers, ports, and local URLs. Track tasks and progress so you can
pick up where you left off in another session or with a coding agent.

## Install

Install the latest stable release. No Go installation required when using the install scripts.

### Windows (PowerShell)

```powershell
irm https://jinyongp.dev/devtools/install.ps1 | iex
```

Open a new terminal, then run `devtools version`. The installer selects x64 or
ARM64 for your PC and adds devtools to your user PATH. No administrator access needed.
Run the same install command again to update an existing installation.
Supports Windows 11 25H2/26H2; see the [Windows guide](docs/windows.md) for details.

### macOS / Linux / WSL

With Homebrew:

```sh
brew install jinyongp/tap/devtools
```

Homebrew also installs `dvt` as a symlink to `devtools` and completion files for
bash, zsh, and fish. See the [completion guide](docs/completion.md) for shell setup.

Or with curl:

```sh
curl -fsSL https://jinyongp.dev/devtools/install.sh | sh
export PATH="$HOME/.local/bin:$PATH"
devtools version
```

For the curl install, add the `export` line to your shell configuration
(such as `~/.zshrc`) to keep it in new terminals. Inside WSL, use this Linux install.

The shorter command `dvt` is also available unless another program already owns
that name. See [installation and updates](docs/install.md) for install locations,
[completion](docs/completion.md) for shell setup, and the [WSL guide](docs/wsl.md)
for using Windows and WSL together. Their data, servers, and CAs stay separate.

## Get started

Run this in a new project directory. `myapp` names the profile that stores your
values. Choose an unused profile name for this example: an existing name shares
its stored values. These first commands work on macOS, Linux, WSL, and Windows.

```sh
devtools init --profile myapp
devtools var set LOG_LEVEL --value info
devtools env create local
devtools var set LOG_LEVEL --env local --value debug
devtools var get LOG_LEVEL --env local
```

The final JSON response contains `data.value: "debug"`, confirming the `local`
override. To pass that value to a child command, choose the example for your shell.

In a POSIX shell on macOS, Linux, or WSL:

```sh
devtools run --env local -- sh -c 'echo "$LOG_LEVEL"'
```

In PowerShell or cmd on Windows:

```powershell
devtools run --env local -- cmd.exe /d /c set LOG_LEVEL
```

These print `debug` and `LOG_LEVEL=debug`, respectively. In your project, put
an installed project tool's command after `--`, such as `pnpm dev` or `go run .`.
Devtools data commands return JSON; child output is not wrapped in JSON.
On builds with [output protection](docs/output-protection.md), non-TTY stdout and
stderr mask delivered secrets by default. TTY output uses the configured policy.

Commit the generated `devtools.toml` to Git. Projects and worktrees that use the
same profile share stored values. Builds with vault support protect stored secrets
with local encryption and a reusable OS-protected or passphrase key session;
public variables remain readable. Complete vault setup before storing secrets. See
[local secret protection](docs/secret-protection.md) for setup and build availability.
The [values guide](docs/cli-contract.md) covers secret registration and saved commands.

Use `devtools --help` or a command's help, such as `devtools task --help`, to
explore the CLI. Run `devtools command list` to see your configured project commands.

### Run a saved command

To save a command you use often, add it to `devtools.toml`. For example, in a
project with a `docs:dev` script:

```toml
[commands."dev:docs"]
exec = ["pnpm", "run", "docs:dev"]
```

Put devtools options before the command name. In devtools v0.23.0 and newer,
the `--` separator before the saved command's arguments is optional:

```sh
devtools run dev:docs --port 3000
devtools run dev:docs -- --port 3000
devtools run --env local dev:docs --port 3000
```

Use `devtools run dev:docs --help` for the saved command's help, or
`devtools run --help` for devtools' own help.

Every argument after the name belongs to the saved command, including `--env`
and `--help`. On older versions such as v0.22.3, use the explicit `--` separator.
See the [command execution guide](docs/cli-contract.md#이름-명령에-추가-인자-전달)
for the version requirements and both forms.

## Guides

The [documentation index](docs/README.md) separates user guides, API contracts,
developer instructions, and architecture references. Most guides are in Korean;
the [task and workstream guide](docs/tasks.md) is in English.

- Values and configuration: [variables, secrets, and commands](docs/cli-contract.md),
  [local secret protection](docs/secret-protection.md), [output protection](docs/output-protection.md),
  [guarded automation and approved local runners](docs/secret-automation.md),
  [environment diagnostics](docs/doctor.md).
- Servers and URLs: [ports](docs/ports.md), [server lifecycle](docs/processes.md),
  [readiness](docs/process-readiness.md), [local domains, HTTPS, and sharing](docs/local-network.md).
- Work coordination: [tasks and workstreams](docs/tasks.md), [Dashboard](docs/dashboard.md). Run
  `devtools dashboard` and open `data.item.url`. The login link is a credential; keep it private.
- Data management: [profiles and transfer](docs/profiles.md),
  [encrypted backups and recovery](docs/backup.md), [storage cleanup](docs/cleanup.md).
- Shells and automation: [completion](docs/completion.md), [CLI output contract](docs/cli-output.md).

## Use with a coding agent

Install the devtools Skill to give your coding agent instructions for setting up
projects, managing servers, and recording progress:

```sh
npx skills add jinyongp/devtools
```

The installer detects your agents and guides you through the install location.
Add `--global` to use the Skill across projects. See [Agent Skill setup](docs/agent-skill.md)
for configuration and updates.

## Update

If you installed with Homebrew, update with Homebrew:

```sh
brew upgrade jinyongp/tap/devtools
```

If you used the install script, run `devtools update` to get the latest stable release:

```sh
devtools update
```

The update replaces the executable and preserves your profile data and project
configuration. Check the new version with `devtools version`.
For a build with vault support, follow [secret setup and migration](docs/secret-protection.md)
before using existing secrets. Updating the executable alone does not encrypt old data.

If you use a coding agent, [update the Skill too](docs/agent-skill.md#업데이트).
The CLI and Skill are updated separately through their respective installers.

devtools is available under the [MIT license](LICENSE).
See the [development guide](docs/development.md) to contribute to the project.
