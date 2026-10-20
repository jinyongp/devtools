# Windows native and WSL boundaries

Check the selected release for matching Windows ZIPs, checksums and installer.
Target Windows 11 25H2/26H2 x64/ARM64.
Actions validates native x64 on Server 2025 (test host, not product support) and
ARM64 on Windows 11; the full CPU/OS matrix and actual-PC UAC/browser/terminal
behavior are unverified. Windows 10 and self-hosted operations are excluded.
Standalone exe needs no Go/Python/Git Bash/WSL runtime. Project tools are separate.

Use a matching install.ps1, ZIP and checksum. Installer inputs are -Version,
-Source, -BinDir, -AddToPath, -NoPath and -Json. It adds the install directory to
user PATH by default; -NoPath disables that change. Default output is human text;
use -Json for machine-readable success/error output. Re-running the installer
updates an owned installation and succeeds when the same version is present.
Native OS architecture selects the package even under x64 PowerShell on ARM64.
Do not change execution policy or shell profiles automatically. dvt.exe is an
owned binary copy. Native update journals old/main/alias replacement and keeps
mapped old images for later cleanup. It never automatically stops active daemons.

Windows private config/data/cache/runtime use Known Folder LocalAppData/devtools,
local NTFS, current-user+SYSTEM protected ACLs. Do not repair foreign ACLs or use
UNC/SMB/reparse/cloud-sync active private storage. Native and WSL stores, daemons,
CAs and ownership are separate. Use encrypted profile transfer; no active process
ownership migrates. WSL uses Linux tools/storage, including appendWindowsPath=false.
Only explicit .exe lookup selects Windows tools. Never modify WSLENV, WSL config,
firewall or portproxy automatically; managed Win32 servers in WSL are unsupported.

Protocol 8 adds diagnostics.paths.source, diagnostics.platform.isolation and safe
storage details on failed issues. Read error.details for operation, resource,
reason and an optional numeric os_error; raw paths and OS messages are excluded.
Known Folder can select a separate sandbox store. Compare actual paths before
interpreting an empty list as missing data. Access denied alone does not identify
the exact ACL or sandbox rule. AppContainer storage creation remains unverified.
Queries and diagnostics can create private lock directories/files.

Read diagnostics.platform and its schema. Unknown interop does not imply failure
or availability; diagnosis never executes Windows children. Linux trust does not
establish Windows browser trust. Export only the public CA, verify its fingerprint,
then deliberately import into the client's CurrentUser Root; preserve existing
certificates. Actual WSL2-to-Windows browser networking remains unverified.

Native custom hosts changes use a constrained same-SID UAC helper; noninteractive
hosts changes need an already elevated token. CurrentUser trust/untrust requires
an interactive terminal and OS root-certificate consent; noninteractive calls
return permission_required. Never automate approval or move CA private keys.
Tailscale requires user-installed/logged-in >=1.104.1 and its fixed protected pipe;
never run debug local-creds on Windows. ARM64 cloudflared uses the user-installed
amd64 provider via Windows emulation. Live accounts/public connectivity are unverified.

Batch arguments reject NUL/newline/double-quote/percent. Run .ps1 with an explicit
PowerShell interpreter. PS5.1 may lose native empty/quote argv before devtools sees
it. completion powershell registers devtools/dvt using literal AST and UTF-8 NUL
stdin; never evaluate dynamic expressions. cmd supports execution only.
Protocol 7 adds diagnostics.platform and the powershell shell; envelope remains
1, TOML remains versionless, and the v6 run/child argument boundary remains intact.
