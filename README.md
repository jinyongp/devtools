# devtools

[English](README.md) | [한국어](README.ko.md)

devtools is a CLI for managing your project's development environment and work.
Store environment variables and secrets, pass them to commands, and manage
development servers, ports, and local URLs. Track tasks and progress so you can
pick up where you left off in another session or with a coding agent.
It runs on macOS, Linux, and WSL.

## Install

If you use Homebrew, install devtools with:

```sh
brew install jinyongp/tap/devtools
```

Homebrew also installs completion files for zsh, bash, and fish. Once shell
completion is enabled, press Tab after `devtools` to explore commands and options.
See the [completion guide](docs/completion.md) for shell setup.

### Install script

Paste the following command into your terminal. The installer detects your
operating system and CPU, installs the latest stable release to
`~/.local/bin/devtools`, and provides the shorter command `dvt`. You need curl.

```sh
curl -fsSL https://jinyongp.dev/devtools/install.sh | sh
```

Add the install directory to your PATH so you can run devtools from anywhere:

```sh
export PATH="$HOME/.local/bin:$PATH"
devtools version
dvt version
```

To keep this setting in new terminals, add the `export` line to your shell's
configuration file, such as `~/.zshrc` for zsh. See the [installation guide](docs/install.md)
for choosing a version or a different install location.

## Get started

Open your project directory and run the example below. `myapp` is the profile
name that identifies your project; replace it with your own project name.

```sh
devtools init --profile myapp
devtools var set LOG_LEVEL --value info
devtools env create local
devtools var set LOG_LEVEL --env local --value debug
devtools run --env local -- sh -c 'echo "$LOG_LEVEL"'
```

This sets `LOG_LEVEL` to `info` by default and overrides it with `debug` in the
`local` environment. The final command prints `debug`, showing that devtools
passed the environment variable to the command. In your project, replace
`sh -c ...` with your usual command, such as `pnpm dev` or `go run .`.

Commit the generated `devtools.toml` to Git. Projects and worktrees that use the
same profile share stored values. Environment variables and secrets are stored
in plaintext files accessible only to your user account; backups are encrypted
separately. See [values and command execution](docs/cli-contract.md) to add
secrets and configure project commands.

Use `devtools --help` or a command's help, such as `devtools task --help`, to
explore the CLI. Run `devtools command list` to see your configured project commands.

## Guides

The detailed guides below are currently in Korean.

| What you want to do | Guide |
| --- | --- |
| Store variables and secrets, import .env files, and run commands | [Values and command execution](docs/cli-contract.md) |
| Check required tools, configuration, and installation status | [Environment diagnostics](docs/doctor.md) |
| Avoid port conflicts and connect to other project URLs | [Ports](docs/ports.md) |
| Give each worktree a `.localhost` URL for its development server | [Local reverse proxy](docs/proxy.md) |
| Run servers, check status and logs, and restart them | [Processes](docs/processes.md) · [Readiness checks](docs/process-readiness.md) |
| Plan work, share tasks, and hand off between sessions | [Tasks and workstreams](docs/tasks.md) |
| Manage environment variables and tasks in a browser | Run `devtools dashboard` and open the URL it prints |
| Compare project settings or move to another computer | [Profiles](docs/profiles.md) |
| Back up and restore data, or remove old data | [Backups](docs/backup.md) · [Cleanup](docs/cleanup.md) |

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

If you use a coding agent, [update the Skill too](docs/agent-skill.md#업데이트).
The CLI and Skill are updated separately through their respective installers.

devtools is available under the [MIT license](LICENSE).
See the [development guide](docs/development.md) to contribute to the project.
