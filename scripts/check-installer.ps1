$ErrorActionPreference = 'Stop'
if (-not ('DevtoolsInstallerCheckArchitecture' -as [type])) {
    Add-Type -TypeDefinition @'
using System;
using System.Runtime.InteropServices;
public static class DevtoolsInstallerCheckArchitecture {
    [DllImport("kernel32.dll", SetLastError = true)]
    public static extern bool IsWow64Process2(IntPtr process, out ushort processMachine, out ushort nativeMachine);
    [DllImport("kernel32.dll")]
    public static extern IntPtr GetCurrentProcess();
}
'@
}
[ushort]$processMachine = 0
[ushort]$nativeMachine = 0
if (-not [DevtoolsInstallerCheckArchitecture]::IsWow64Process2([DevtoolsInstallerCheckArchitecture]::GetCurrentProcess(), [ref]$processMachine, [ref]$nativeMachine)) { throw 'Cannot resolve native OS architecture.' }
switch ([int]$nativeMachine) { 34404 { $arch = 'amd64' }; 43620 { $arch = 'arm64' }; default { throw 'Unsupported native architecture.' } }
$commit = (& git rev-parse HEAD).Trim()
if ($LASTEXITCODE -ne 0) { throw 'Cannot resolve checkout identity.' }
& pwsh -NoProfile -NonInteractive -File scripts/test-windows-install.ps1 -Arch $arch -Commit $commit
if ($LASTEXITCODE -ne 0) { throw 'Windows installer checks failed.' }
