# Windows

[Documentation](README.md) · [Quick start](../README.md#install)

## Install

Run in PowerShell to install the latest stable release:

```powershell
irm https://jinyongp.dev/devtools/install.ps1 | iex
```

The installer explains what devtools does, shows its location, and gives the next
command to run. Open a new terminal, then check:

```powershell
devtools version
```

The installer picks x64 or ARM64 for your PC and adds devtools to your user PATH.
Run the same command again to update an existing installation. Repeating it when
the latest stable release is already installed also succeeds.
No administrator access, Go, Python, Git Bash, or WSL required. Install your project's
tools separately. Inside WSL, use the [Linux installation](install.md#macos-linux-and-wsl).

## First use

In your project directory, choose an unused profile name for this example:

```powershell
devtools init --profile myapp
devtools var set LOG_LEVEL --value info
devtools run -- cmd.exe /d /c set LOG_LEVEL
```

The last command prints `LOG_LEVEL=info`. Projects using the same profile share
stored values. See the [root README](../README.md#get-started) for the next steps.

<a id="후보-설치와-업데이트"></a>

## Updates and recovery

```powershell
devtools update
```

Updates run natively without a Unix shell. Restart active daemons and managed
processes to use the new executable. Running images are retained under unique old
names while a journal protects main and alias replacement. After interruption,
retry the same installer or update. Preserve retained files; a later update can
clean up old images once they are no longer in use.

For a custom directory or an offline release, see [installation options](install.md#custom-installation).
The default directory is LocalAppData's `Programs\devtools\bin`; use the reported
path if Known Folders are redirected. `dvt.exe` is a separate copy of the executable.
An existing external `dvt` is preserved and reported as `skipped_conflict`.

## Troubleshooting installation

- **Command not found:** open a new terminal after installation. The installer updates
  your user PATH by default, not the current session or system PATH. With `-NoPath`,
  add the reported directory yourself.
- **Policy blocks execution:** follow your organization's PowerShell policy. The
  installer does not change execution policy or shell profiles.
- **Storage rejected:** keep private data on local NTFS with permissions for the
  current user and SYSTEM; an AppContainer also needs its own container SID.
  Network shares and cloud-synced storage are unsupported.

<a id="지원과-검증-범위"></a>

## Supported systems and validation

| Item | Scope |
| --- | --- |
| Supported Windows | Windows 11 25H2/26H2, x64 and ARM64, current cumulative updates |
| Shells | PowerShell 5.1/7 and cmd; standalone `devtools.exe` and shorter `dvt.exe` |
| Automated validation | Native GitHub Actions on `windows-2025` x64 and `windows-11-arm` ARM64; Server is a test host, not a supported product OS |
| Excluded | Windows 10, Windows 11 24H2 or older, the special 26H1 release, LTSC/IoT/Insider, 32-bit Windows, and self-hosted runner operation; future OS versions are not automatically supported |
| Unverified | Actual-PC UAC, browser trust, terminal closure and observed installation; the full OS/CPU matrix; final WSL2/Windows browser networking; live provider accounts and LAN |

ARM64 CLI and Node.js run natively; Chromium coverage uses x64 emulation. Actions
success does not establish actual-PC coverage for every supported OS/CPU combination.

<a id="저장소와-프로세스"></a>

## Storage and processes

Config, data, cache, and runtime directories are separate under LocalAppData's
`devtools` directory. Private storage requires local NTFS with access restricted to
the current user and SYSTEM. Inside an AppContainer, the private storage policy
also grants access to that process's specific container SID. Creation and ACL
validation use the same identity policy for directories, files and locks; broad
application-package grants and other containers' SIDs are rejected. Known Folders
may select an AppContainer's separate LocalAppData directory, so its storage can
differ from an ordinary PowerShell session. This storage behavior does not imply
that the sandbox permits every command or external resource.
Unsupported ACLs, including existing objects created with a different identity
policy, are not repaired automatically.
For paths inside the container's OS-selected LocalAppData, ancestor pinning starts
at that Known Folder. Its resolved handle must match the selected path; every
component below it is pinned and checked for reparse points. The owning host user
and SYSTEM remain trusted. Paths outside this boundary and ordinary processes
retain the drive-root traversal. Access denial never causes a traversal check to
be skipped.
UNC/SMB, device paths, alternate data streams, reparse points/symlinks, and cloud-synced
folders are unsupported for active private storage. In the current var/sec source,
secret values use local encryption with user DPAPI protection; public variables and
metadata remain readable. Check [vault setup and build availability](secret-protection.md).
Backups and profile exports are encrypted separately. Use [profile transfer](profiles.md) to
move data between computers or operating systems.

Managed processes join an owned Job at creation. Shutdown requests graceful exit,
then terminates the remaining tree after three seconds. A parent host Job still
applies: closing the host can end its children. Devtools does not terminate processes
based only on saved PIDs. If supervisor ownership cannot be confirmed, it returns
an unknown/recovery state and leaves external processes alone.

Project commands are argv arrays. `.cmd`/`.bat` files use the OS's
`cmd.exe /d /e:on /v:off /s /c`; batch arguments containing NUL, newlines, double
quotes, or percent signs are rejected. Use an executable directly for complex values.
For `.ps1`, include `pwsh.exe` or `powershell.exe` explicitly in argv.
PowerShell 5.1 may lose empty or quoted native arguments before the CLI receives
them; use PowerShell 7 native argument mode when needed.

<a id="로컬-https와-권한"></a>

## Local HTTPS and permissions

Start with loopback high ports in the [local network guide](local-network.md).
Windows setup diagnoses sockets and installation images and records intent; it does
not change firewall rules, HTTP.sys, or portproxy. Custom hosts edits use a restricted
UAC helper for the same SID. Elevation as another user is unsupported; noninteractive
hosts edits require an already elevated token.

CA trust is explicit: `devtools proxy trust --request-id UUID` registers the CA in
CurrentUser Root. Trust and untrust require an interactive terminal and OS certificate
confirmation; noninteractive calls return `permission_required`. Devtools does not
auto-approve OS prompts or adopt external certificates. Removal requires its own
matching receipt and fingerprint. You can also export the public CA for a client
without adding it to OS trust. Actual-PC browser trust remains unverified.

WSL Linux trust, Windows browser trust, and the native Windows CA are separate.
See the [WSL guide](wsl.md) for public-CA export and client import.

## Sharing and completion

Tailscale requires a user-installed, authenticated client at version 1.104.1 or
later. It uses a fixed protected named pipe and ETag/If-Match to update only the
selected Serve handler. Devtools does not install or log in to Tailscale, run its
debug credential proxy, or overwrite unrelated configuration. Install cloudflared
separately too; ARM64 uses its amd64 executable through Windows emulation.
Provider tests use real pipes and fixture children; live public accounts remain
unverified.

[PowerShell completion](completion.md#powershell-51--7) supports both `devtools`
and `dvt` in PowerShell 5.1/7. cmd supports execution only. Completion reads literal
AST values without evaluating dynamic expressions.
