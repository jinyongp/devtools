# WSL

[Documentation](README.md) · [Quick start](../README.md#install)

Install Linux devtools and Linux project tools inside WSL. Windows and WSL have
separate data, daemons, CAs, and managed servers. Use encrypted profile export/import
to transfer logical data; live process ownership is not transferred.

## Install and check

Run the [Linux installer](install.md#macos-linux-and-wsl) in your WSL terminal.
Confirm that your shell selects the Linux binary rather than Windows `devtools.exe`:

```sh
command -v devtools
devtools version
devtools diagnostics
```

Diagnostics reports WSL in `data.platform.runtime`. Install and locate project
tools in a WSL shell that loads your shell initialization.

<a id="프로젝트와-저장소"></a>

## Projects and storage

Projects may live under `/home/...` or `/mnt/c/...`. Keep private config, data,
cache, and the Linux CLI with configured permissions on the Linux filesystem.
DrvFS permission displays under `/mnt/c` do not establish private-storage safety.
WSL uses Linux XDG paths; it does not discover or import Windows LocalAppData.

`appendWindowsPath=false` is supported. Tools such as `node`, `pnpm`, and `git`
are resolved from Linux PATH; Windows `.exe` files are not selected as substitutes.
To run a Windows program in the foreground, specify `.exe` or an absolute path.
Win32 programs cannot run as managed WSL servers; manage Windows servers with native
Windows devtools. Foreground interop cannot guarantee cross-OS descendant cleanup.

Devtools does not create or modify WSLENV. Opt in only the variables you intend to
share. For example, set `WSLENV=PUBLIC_CANARY/u` in your shell for a public value;
never use a secret as a test canary.

## Diagnostics

On builds with vault support, WSL uses its own Linux key protection. Use an
existing Secret Service or explicitly select the passphrase backend for a
keyringless distro. Windows DPAPI does not unlock the WSL vault. See
[local secret protection](secret-protection.md) for setup, session reuse, and recovery.

```sh
devtools diagnostics
devtools schema diagnostics
```

`data.platform` reports `os`, `arch`, `runtime`, `storage_filesystem`,
`storage_supported`, `interop`, `client_trust`, and `limitations`. In WSL,
`client_trust` is `separate_windows_store`.

Default diagnostics runs no Windows program or network probe. Interop may be
`unknown` even when registered as enabled; an explicitly disabled registration is
`unavailable`. Unknown interop alone does not make the overall report unready.
A confirmed unsupported private filesystem returns `ready: false` with an
`unsupported_storage` issue. Unknown filesystem detection is not proof of support;
check ACL and symlink results too.

Diagnostics does not infer networking mode or print raw WSL configuration or
environment values. It does not change WSL settings, firewall rules, portproxy,
PATH, or distro state.

## Open a WSL service in a Windows browser

First check the high-port example in the [local network guide](local-network.md)
inside WSL. Check Windows IPv4 localhost forwarding, name resolution, and certificate
trust separately. A working `.localhost` address does not establish custom-host,
IPv6, reverse, or LAN connectivity. Configure Windows DNS/hosts separately for
custom names; changing WSL hosts does not change Windows hosts.

Linux `trusted: true` does not establish Windows browser trust. Export only the
public CA from WSL and copy it to Windows without overwriting an existing file:

```sh
devtools proxy ca export --output ./devtools-wsl-ca.pem
```

Record the returned SHA-256 fingerprint separately. Do not copy private keys or
Linux private data. The following is a manual Windows PowerShell 5.1/7 example.
Replace the path and `EXPECTED_SHA256`. Windows may require OS certificate
confirmation; import into CurrentUser Root, not the system-wide store.

```powershell
$path = 'C:\Temp\devtools-wsl-ca.pem'
$expected = 'EXPECTED_SHA256'
$cert = [Security.Cryptography.X509Certificates.X509Certificate2]::new($path)
$sha = [Security.Cryptography.SHA256]::Create()
try { $actual = ([BitConverter]::ToString($sha.ComputeHash($cert.RawData))).Replace('-', '').ToLowerInvariant() }
finally { $sha.Dispose() }
if ($actual -ne $expected.ToLowerInvariant()) { throw 'CA fingerprint mismatch' }
$wasPresent = Test-Path ('Cert:\CurrentUser\Root\' + $cert.Thumbprint)
if (-not $wasPresent) {
  Import-Certificate -FilePath $path -CertStoreLocation Cert:\CurrentUser\Root
}
$cert.Thumbprint
```

Restart the browser and request HTTPS with the correct hostname and port. Keep
certificate verification enabled. For removal, select only the certificate newly
imported by this procedure. If `$wasPresent` was true, leave it in place. Use the
exact recorded thumbprint; do not delete certificates by a shared name.

```powershell
if (-not $wasPresent) {
  Remove-Item -LiteralPath ('Cert:\CurrentUser\Root\' + $cert.Thumbprint)
}
$cert.Dispose()
```

This CA belongs to WSL; native Windows `proxy untrust` does not remove it. Initialize
and manage a native Windows CA separately if needed.

## Validation limits

Product validation uses Linux CI and WSL diagnostic fixtures. The Release workflow
also checks vault sessions across an owned WSL2 distro stop/restart and durable
manual lock on disposable Actions hosts. See [native validation](development.md#varsec-통합-수용-검증).
That vault test does not validate cross-OS networking. Final WSL2 NAT/mirrored
networking, Windows browser access, IPv6, reverse connections, other distros, LAN,
ARM64 WSL2, and the manual certificate procedure above have not been tested on an
actual PC. Standalone loopback probes do not establish coverage for these environments.
The presence of `wsl.exe` on an Actions runner does not prove WSL2 startup or network
behavior. Validation does not stop the current distro or change its networking mode.

References: [Import-Certificate](https://learn.microsoft.com/en-us/powershell/module/pki/import-certificate?view=windowsserver2025-ps),
[WSL interop configuration](https://learn.microsoft.com/en-us/windows/wsl/wsl-config),
[WSLENV](https://learn.microsoft.com/en-us/windows/wsl/filesystems#share-environment-variables-between-windows-and-wsl-with-wslenv).
