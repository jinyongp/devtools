# devtools

Agent-first developer tools in one Go CLI. Manage profile variables and secrets,
select environments, and run project commands with inherited values. Commands
provide JSON discovery and results; `run` passes through child process output.

See [사용법과 CLI 계약](docs/cli-contract.md) for the profile, variable,
secret, environment, and execution interfaces, including named commands in TOML.
See [설치와 업데이트](docs/install.md) for release packaging, the installer,
and installed-Linux verification through Docker.
The [Linux sandbox](docker/README.md) runs isolated verification scenarios with
`just verify-docker`.

Task and workstream commands coordinate specifications, dependencies, execution
claims, recovery checkpoints, and validation evidence across worktrees.
See the [usage guide](docs/tasks.md), [API contract](docs/task-api-contract.md),
and [agent skill](skills/devtools/SKILL.md).

`devtools dashboard` returns an authenticated local link to the D3 Canvas graph.
Profiles, workstreams, and details load as you select them. Create and edit tasks,
manage dependencies and checkpoints, and edit common values and environment
overrides from the same session. See the [management API](docs/management-api-contract.md).

See the [management roadmap](docs/management-roadmap.md) for process management,
storage cleanup, and the next dashboard integrations.

`devtools backup` encrypts profile values and task history with a public key and
restores whole profiles with preview and retry protection. See [backup and recovery](docs/backup.md).

`devtools process` starts named project commands in the background and manages
their lifecycle across agent sessions. The dashboard provides the same start,
stop, and restart actions. See [process management](docs/processes.md).

`devtools cleanup` previews expired and inactive storage, archives selected items,
and restores or purges archives. See [storage cleanup](docs/cleanup.md).

`devtools doctor` diagnoses project requirements, exact tool versions, and
required variable/secret registrations. Named commands check declared
prerequisites before running. See [environment diagnosis](docs/doctor.md).

`devtools port` persists local TCP assignments per execution location.
Commands declare `serve` to start services and `bind` to inject ports or compose
URLs. See the [port guide](docs/ports.md) and [contract](docs/port-design.md).

## Install and update

[GitHub Releases](https://github.com/jinyongp/devtools/releases) distributes
macOS and Linux binaries for amd64 and arm64. WSL uses the Linux binary.
The installer selects the platform and installs the latest stable release into
`~/.local/bin/devtools`. Downloads require curl and HTTPS access.

```sh
(
  devtools_install_dir=$(mktemp -d "${TMPDIR:-/tmp}/devtools-install.XXXXXXXX") || exit 1
  trap 'rm -rf "$devtools_install_dir"' EXIT
  trap 'exit 130' INT
  trap 'exit 143' TERM HUP
  curl --fail --silent --show-error --location --proto '=https' --proto-redir '=https' \
    https://github.com/jinyongp/devtools/releases/latest/download/install.sh \
    -o "$devtools_install_dir/install.sh" || exit 1
  sh "$devtools_install_dir/install.sh" install
)
export PATH="$HOME/.local/bin:$PATH"
devtools version

# Update to the latest stable release.
(
  devtools_install_dir=$(mktemp -d "${TMPDIR:-/tmp}/devtools-install.XXXXXXXX") || exit 1
  trap 'rm -rf "$devtools_install_dir"' EXIT
  trap 'exit 130' INT
  trap 'exit 143' TERM HUP
  curl --fail --silent --show-error --location --proto '=https' --proto-redir '=https' \
    https://github.com/jinyongp/devtools/releases/latest/download/install.sh \
    -o "$devtools_install_dir/install.sh" || exit 1
  sh "$devtools_install_dir/install.sh" update
)
```

Add `--version 0.1.0` to the installer's `install` or `update` invocation to
select a release. Each invocation downloads into a private temporary directory
and removes it when finished.

Include the same PATH in the agent's process configuration. Installation and
updates run noninteractively and return JSON. Updates preserve profile data and
project configuration. See [installation details](docs/install.md) for custom
directories, local artifacts, and release verification.

## Publishing releases

Pushing a version tag such as `v0.1.0` starts GitHub Actions validation, packages
all four targets, and publishes the archives, SHA-256 checksums, `install.sh`, and
`version.txt` to GitHub Releases. Tags such as `v0.2.0-rc.1` publish prereleases;
the installer selects GitHub's latest stable release by default.

## Build and verify

Requires Go 1.27.x, Git (for integration tests), and just.

```sh
just build
just check
./bin/devtools version
./bin/devtools schema
./bin/devtools project inspect
```

`just check` checks formatting, runs `go vet`, and runs tests with the race
detector. CI runs on macOS and Linux with the latest Go 1.27 patch. WSL uses
the Linux build; testing in an actual WSL environment is a separate check.

## Project configuration

Initialize the current directory with an explicit profile:

```sh
devtools init --profile myapp
```

The command returns `created`, `config_path`, and `profile` in its JSON data.
An existing valid configuration with the same profile returns `created: false`
without changing its contents. A different profile returns `profile_conflict`;
malformed configuration returns `invalid_config`. Existing files are preserved.
The command operates only in the current directory, creates no global data,
and does not stage files in Git. Concurrent initializers publish a complete
file without replacing an existing path. The filesystem must support hard links.

Commit `devtools.toml` to the project repository:

```toml
profile = "myapp"
```

The profile is a case-sensitive, user-chosen project identifier shared by tools.
It contains 1–128 ASCII letters, digits, dots, underscores, or hyphens and begins
with a letter or digit. Configuration compatibility is managed by devtools;
users do not supply a configuration version. Unknown fields are rejected to
catch typos. Secret values belong in user storage, not this file.

`project inspect` searches upward from the current directory (or `--dir`) for
the nearest configuration. It includes the directory containing `.git` but
does not search above it. Both `.git` directories and worktree `.git` files
form boundaries. Outside Git, the search ends at the filesystem root.
Symlinks in the starting directory are resolved before searching.

Tracked configuration is checked out in each worktree. Each worktree reads its
own copy, so branch-specific changes take effect without consulting another
worktree. `root` in the result is the directory containing the selected config.

```sh
./bin/devtools project inspect --dir /path/to/worktree
./bin/devtools project inspect --profile myapp
```

An explicit profile bypasses filesystem configuration lookup, including invalid
configuration. The result identifies its source as `flag` or `file` and includes
the configuration path when applicable. Inspection computes user directories
without creating them or reading secrets.

| OS | Config | Data | Cache |
| --- | --- | --- | --- |
| macOS | `~/Library/Application Support/devtools` | Config path + `/data` | `~/Library/Caches/devtools` |
| Linux / WSL | `$XDG_CONFIG_HOME/devtools` | `$XDG_DATA_HOME/devtools` | `$XDG_CACHE_HOME/devtools` |

Unset or relative XDG values fall back to `~/.config`, `~/.local/share`, and
`~/.cache`, respectively. macOS uses its native directories.

## Import an existing .env file

Import mixed variables and secrets by passing the file path and public key names:

```sh
devtools env create local
devtools import --file .env.local --env local --var NODE_ENV --var PORT --dry-run
devtools import --file .env.local --env local --var NODE_ENV --var PORT
```

Existing keys retain their kinds. New keys default to secret; repeated `--var`
options select public variables. Omit `--env` for common values, or use
`--profile NAME` to select a profile explicitly. The selected env must exist.

Import validates the entire file and commits all entries atomically. Identical
values succeed unchanged; `--overwrite` allows replacing different values in
the selected scope. `--dry-run` reports key names, kinds, and actions with an
`applicable` flag. Responses contain metadata only, and the source file stays
in place. See the [CLI contract](docs/cli-contract.md) for dotenv syntax and
conflict handling.

## Agent contract

Commands accept CLI flags and positional arguments. Secret input uses a regular
file or piped stdin containing the raw UTF-8 value, including trailing newlines.
`schema` returns command definitions, aliases, parsed input schemas,
data output schemas, and the response envelope schema. Command output schemas
reference the catalog's `$defs`; preserve those definitions when validating a
command's data independently. `help`, no arguments, and `--help` return the same
catalog. `<command> --help` returns that command's definition.

Successful data commands write one JSON envelope to stdout and exit 0:

```json
{"schema_version":1,"ok":true,"data":{"version":"dev","commit":"unknown"}}
```

Data command failures and execution setup failures write a JSON envelope to stderr:

```json
{"schema_version":1,"ok":false,"error":{"code":"invalid_argument","message":"Unknown command. Run devtools schema to discover commands."}}
```

Output writer failures can leave partial output. Consumers should always check
the exit status before decoding a successful result. Error messages are for
diagnostics; branch on the stable `error.code` field. Argument errors do not
echo unrecognized values. Response schema versions are generated by the tool.

| Exit | Error codes |
| --- | --- |
| 0 | Success |
| 1 | `io_error` |
| 2 | `invalid_argument` |
| 3 | `project_not_found`, `invalid_config`, `profile_conflict` |
| 130 | `canceled` |

Value commands also report `storage_error` with exit 1 and `env_not_found`,
`env_not_empty`, `key_not_found`, `kind_conflict`, or `invalid_storage` with exit 3.
Import reports `invalid_dotenv` with exit 2 and `import_conflict` with exit 3.
Repeatable options such as import's `--var` appear as arrays in input schemas.
An undefined project command exits 3. Missing executables exit 127 and startup
failures exit 126. `run` preserves the child's exit status and raw stdout/stderr;
signal exits use 128 + signal number. Execution uses a process group and forwards
SIGINT, SIGTERM, and SIGHUP, with forced termination after a 3-second grace period.

## Adding a tool

- Add the tool's behavior in its own `internal/` package.
- Register commands in `internal/cli`. Options generate flag parsing, validation,
  help, and schema discovery from the same definition.
- Return data or a `protocol.Error` from handlers. The runner serializes results.
- Resolve project context only for commands that need it. Use `paths` for user
  storage locations. Handlers receive context and injectable input/output.
- Test valid input, failure behavior, and command-specific risks.

Profile data lives under the user data directory in a private `profiles` folder.
Directories use mode 0700 and files mode 0600. Secrets are stored as plaintext
with user-only filesystem access. The CLI exposes secret metadata and process
injection; same-user file access and secrets printed by child programs remain
within the user's access boundary. Updates are locked and published atomically.

Build metadata defaults to `dev` / `unknown`; release builds may set it:

```sh
go build -ldflags '-X main.version=0.1.0 -X main.commit=COMMIT_SHA' -o bin/devtools ./cmd/devtools
```
