# Windows native and WSL

## Select the execution environment

- WSL workspace: use Linux binaries/storage and initialized WSL shell. Native Windows and WSL stores, daemons, CA and process ownership are separate.
- WSL uses `appendWindowsPath=false`; only explicit `.exe` lookup selects Windows tools. Managed Win32 servers in WSL are unsupported.
- Do not change WSLENV, WSL configuration, firewall or portproxy automatically. Transfer profiles through encrypted archives, not live storage copies.
- Unknown interop does not imply availability/failure. Diagnostics do not execute Windows children.
- Linux trust does not establish Windows browser trust. Export only the public CA, verify fingerprint and deliberately import into CurrentUser Root; preserve existing certificates.
- Actual WSL2-to-Windows browser connectivity remains unverified; inspect both sides separately.

## Install or update only when requested

- Match release, Windows ZIP, architecture, checksum and installer. Native OS architecture selects the package, including x64 PowerShell on ARM64.
- Target Windows 11 25H2/26H2 x64/ARM64; Windows 10 and self-hosted operations are excluded.
- CI covers x64 on Server 2025 (test host) and ARM64 on Windows 11. Full CPU/OS combinations and actual-PC UAC/browser/terminal behavior remain unverified.
- Standalone exe needs no Go/Python/Git Bash/WSL; project tools are separate prerequisites.
- Installer inputs: `-Version`, `-Source`, `-BinDir`, `-AddToPath`, `-NoPath`, `-Json`. Default adds user PATH and returns human text; `-NoPath` avoids PATH changes.
- Re-running updates owned installation; same version succeeds. Never silently change execution policy or shell profiles.
- `dvt.exe` is an owned copy. Updates journal old/main/alias replacement, retain mapped old images for cleanup and do not stop active daemons automatically.

## Storage and sandbox identity

- Native private storage: Known Folder LocalAppData/devtools, local NTFS, current-user/SYSTEM protected ACLs.
- AppContainer storage additionally grants its specific SID; broad package grants and other container SIDs are rejected. Do not repair foreign ACLs or use UNC/SMB/reparse/cloud-sync private storage.
- Protocol 8 adds `diagnostics.paths.source`, platform isolation and safe storage issue metadata. Compare actual paths before treating an empty list as missing data.
- `error.details` can contain operation, resource, reason and numeric `os_error`; raw paths/OS messages are excluded. Access denied alone does not identify the exact ACL/sandbox restriction.
- Identity checks apply to directories/files/locks and do not override host sandbox rules. Queries/diagnostics may create private lock directories/files.

## Host permissions and providers

- Native hosts changes use a same-SID UAC helper; noninteractive changes need an already elevated token.
- CurrentUser trust/untrust needs interactive terminal and OS root-certificate consent; noninteractive returns `permission_required`. Never automate consent or move CA private keys.
- Tailscale needs user-installed/logged-in >=1.104.1 and its fixed protected pipe. Never run `debug local-creds` on Windows.
- ARM64 cloudflared uses the user-installed amd64 provider through emulation. Live accounts/public connectivity remain unverified.

## Shell boundaries

- Batch args reject NUL/newline/double-quote/percent. Run `.ps1` with an explicit PowerShell interpreter.
- PS5.1 may lose native empty/quote argv before devtools sees it. Prefer explicit files when exact bytes matter.
- PowerShell completion uses literal AST plus UTF-8 NUL stdin; never evaluate dynamic expressions. cmd supports execution only.
- Protocol 7 adds the PowerShell shell/platform diagnostics; envelope stays 1 and TOML stays versionless. Run options still precede the configured command name.
