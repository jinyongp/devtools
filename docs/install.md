# Installation and updates

[Documentation](README.md) · [Quick start](../README.md#install)

These instructions install the latest stable release. Install scripts use prebuilt
binaries; Go is only needed when building from source. Project tools such as Node.js
or Python are installed separately.

## Windows

Run in PowerShell:

```powershell
irm https://jinyongp.dev/devtools/install.ps1 | iex
```

Open a new terminal and check the installation:

```powershell
devtools version
```

The installer selects your PC's native x64 or ARM64 architecture, installs under
LocalAppData in `Programs\devtools\bin`, and adds that directory to your user PATH.
It runs as your normal user. See the [Windows guide](windows.md) for supported
Windows editions, storage requirements, and troubleshooting.

Run the same command again to update an existing installation to the latest stable
release. Repeating it when that release is already installed also succeeds.

## macOS, Linux, and WSL

With Homebrew:

```sh
brew install jinyongp/tap/devtools
```

Homebrew also installs `dvt` as a symlink to `devtools` and completion files for
bash, zsh, and fish. Both command names work without editing shell configuration.
The Formula declares the link through `command_aliases`; Homebrew manages its
updates and removal.

Or install with curl:

```sh
curl -fsSL https://jinyongp.dev/devtools/install.sh | sh
export PATH="$HOME/.local/bin:$PATH"
devtools version
```

The script installs to `~/.local/bin`. Add the `export` line to your shell
configuration, such as `~/.zshrc`, to use devtools in new terminals. Give agent
processes the same PATH. Inside WSL, install this Linux binary; Windows and WSL
have separate installations and data.

Both installers select the archive for your OS and CPU, then verify checksums,
archive contents, and the executable's version before installation. Get installers and assets from a trusted source.
The shorter name `dvt` is provided too. An existing command with that name is
preserved, and installation can still succeed with `skipped_conflict`.

## Update

For a script installation, run on any supported platform:

```sh
devtools update
devtools version
```

For Homebrew:

```sh
brew upgrade jinyongp/tap/devtools
```

Homebrew manages its own files; `devtools update` returns `package_managed` for
that installation. Restart managed processes, the proxy daemon, and the dashboard
to use the updated executable. Your profile data and `devtools.toml` are preserved.
Downgrading the binary does not undo data-format upgrades; restore a compatible
backup into a fresh data directory if you need an older release.

For builds with vault support, updating the binary does not encrypt old secrets.
Follow [local secret setup and migration](secret-protection.md#기존-저장소를-전환하기)
before using them. Keep the existing key material and recovery backup. On macOS,
a changed application signature can require explicit Keychain approval with
`devtools vault unlock --interactive`; installation success does not grant that approval.

Update the Agent Skill separately if you installed it:

```sh
npx skills update devtools
```

Use `--global` for a global Skill installation. See [Agent Skill setup](agent-skill.md).

## Custom installation

### Choose a directory

Download the installer if you want to inspect it or supply options. On Windows:

```powershell
Invoke-WebRequest 'https://jinyongp.dev/devtools/install.ps1' -OutFile '.\install.ps1'
.\install.ps1 -BinDir 'C:\Tools\devtools'
```

Running a downloaded script is subject to your execution policy. The quick install
above executes the script in the current PowerShell session; it does not change
execution policy. Organization policies still apply.

On macOS, Linux, or WSL:

```sh
curl -fsSL https://jinyongp.dev/devtools/install.sh | sh -s -- --bin-dir "$HOME/bin"
```

For the Unix installer, add the chosen directory to PATH. The Windows installer
adds it to your user PATH by default. To leave PATH unchanged, use `-NoPath` with
a downloaded installer. The existing `-AddToPath` option remains supported;
`-NoPath` takes precedence. The installer does not change the current session,
shell profiles, execution policy, or system PATH. If Known
Folders are redirected, use the actual path reported by the installer.

### Automation output on Windows

The Windows installer explains what was installed and how to start using it.
For scripts that need machine-readable output, download the installer and pass `-Json`:

```powershell
.\install.ps1 -Json
```

Success writes one JSON response to stdout. Failure writes a JSON error to stderr
and exits with a nonzero status. Progress and getting-started messages are omitted
in this mode. The response reports `data.action` as `install` for a new installation
or `update` for an existing one. Regular devtools data commands continue to return JSON.

### Choose a release or use offline assets

Version selection is optional. Replace `VERSION` with the release number without
its `v` prefix; replace paths with the directory containing that release's assets.
Keep each installer, archive, and checksum from the same release together.

On Windows, with a downloaded installer:

```powershell
.\install.ps1 -Version VERSION -Source 'C:\Temp\devtools-release'
```

On macOS, Linux, or WSL, with a downloaded installer:

```sh
sh install.sh --version VERSION --source ./devtools-release
```

`-Source` / `--source` also accepts an HTTPS directory. A custom source requires
an explicit version. For an installed CLI, use
`devtools update --version VERSION --source DIRECTORY`.
`DEVTOOLS_RELEASE_URL` can select an HTTPS mirror with GitHub-compatible
`latest/download/version.txt` and `download/vVERSION/FILE` paths.

## Installation recovery

Windows preserves the running image under a unique old filename and journals file
replacement. Retry the same installer or update after interruption; do not manually
remove retained files. See [Windows updates](windows.md#updates-and-recovery).

For the Unix installer, use `update` to replace an existing installation:

```sh
curl -fsSL https://jinyongp.dev/devtools/install.sh | sh -s -- update
```

Pass the same `--bin-dir` if you used a custom directory. Updates to one directory
are serialized by an installation lock. If a killed installer leaves an empty
`.devtools-install.lock` directory, verify no installer is running before removing
that lock and retrying. Checks fail before replacing the existing executable.

## Building and publishing

Maintainer procedures belong in the [development guide](development.md#릴리스).
See [installed-release verification](../verify/README.md) for isolated installation,
update, and data-preservation scenarios. Windows execution is verified in native
GitHub Actions jobs; actual-PC verification limits are listed in the Windows guide.
