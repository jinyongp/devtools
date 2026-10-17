param([Parameter(Mandatory=$true)][string]$Arch, [Parameter(Mandatory=$true)][string]$Commit)
$ErrorActionPreference = 'Stop'
$temporaryBase = $env:RUNNER_TEMP
if (-not $temporaryBase) { $temporaryBase = [IO.Path]::GetTempPath() }
$root = Join-Path $temporaryBase ('devtools-install-check-' + [Guid]::NewGuid().ToString('N'))
New-Item -ItemType Directory -Path $root | Out-Null
$releases = Join-Path $root 'release files 한글'
$installer = Join-Path $PWD 'scripts/install.ps1'
$oldVersion = $env:VERSION; $oldCommit = $env:COMMIT; $oldOutput = $env:OUTPUT_DIR; $oldOS = $env:TARGET_OS; $oldArch = $env:TARGET_ARCH
function Require([bool]$Condition, [string]$Message) { if (-not $Condition) { throw $Message } }
function Invoke-Json([string]$Executable, [string[]]$Arguments) {
    $text = & $Executable @Arguments 2> (Join-Path $root 'diagnostic.txt')
    Require ($LASTEXITCODE -eq 0) ('Command failed; see disposable diagnostic file: ' + $Executable)
    return ($text -join "`n" | ConvertFrom-Json)
}
try {
    $env:OUTPUT_DIR = $releases; $env:COMMIT = $Commit; $env:TARGET_OS = 'windows'; $env:TARGET_ARCH = $Arch
    foreach ($version in @('0.0.0-test.1', '0.0.0-test.2')) {
        $env:VERSION = $version
        & python -B scripts/package.py
        Require ($LASTEXITCODE -eq 0) 'Cannot prepare native install release.'
    }
    $shells = @((Get-Command powershell.exe).Source, (Get-Command pwsh.exe).Source)
    $emulatedShell = $null
    if ($Arch -eq 'arm64') {
        # Pinned official portable test fixture; no system installation or PATH change.
        $archive = Join-Path $root 'PowerShell-7.6.6-win-x64.zip'
        Invoke-WebRequest -UseBasicParsing -Uri 'https://github.com/PowerShell/PowerShell/releases/download/v7.6.6/PowerShell-7.6.6-win-x64.zip' -OutFile $archive -TimeoutSec 120
        Require ((Get-FileHash -LiteralPath $archive -Algorithm SHA256).Hash.ToLowerInvariant() -ceq '02fe458be20493fbdf43f61ea20610b811ee6c738ab1676c61b9cfcd1a33c860') 'Official x64 PowerShell fixture checksum mismatch.'
        $portable = Join-Path $root 'powershell-x64'
        Expand-Archive -LiteralPath $archive -DestinationPath $portable
        $emulatedShell = Join-Path $portable 'pwsh.exe'
        $actual = Invoke-Json $emulatedShell @('-NoProfile', '-NonInteractive', '-Command', '[pscustomobject]@{process=[Runtime.InteropServices.RuntimeInformation]::ProcessArchitecture.ToString();os=[Runtime.InteropServices.RuntimeInformation]::OSArchitecture.ToString()} | ConvertTo-Json')
        Require ($actual.process -eq 'X64' -and $actual.os -eq 'Arm64') 'ARM64 fixture did not run actual x64 emulation.'
        $shells += $emulatedShell
    }
    $results = @()
    foreach ($shell in $shells) {
        $shellVersion = Invoke-Json $shell @('-NoProfile', '-NonInteractive', '-Command', '$PSVersionTable.PSVersion.ToString() | ConvertTo-Json')
        $bin = Join-Path $root ('설치 경로 ' + [Guid]::NewGuid().ToString('N') + '/bin')
        $beforePath = [Environment]::GetEnvironmentVariable('Path', 'User')
        $installed = Invoke-Json $shell @('-NoProfile', '-NonInteractive', '-ExecutionPolicy', 'Bypass', '-File', $installer, '-Version', '0.0.0-test.1', '-Source', $releases, '-BinDir', $bin)
        Require ($installed.ok -and $installed.data.action -eq 'install') 'Bootstrap install response mismatch.'
        Require ([Environment]::GetEnvironmentVariable('Path', 'User') -ceq $beforePath) 'Installer changed user PATH without AddToPath.'
        $main = Join-Path $bin 'devtools.exe'; $alias = Join-Path $bin 'dvt.exe'
        Require ([IO.File]::Exists($main) -and [IO.File]::Exists($alias)) 'Installer did not publish both executable copies.'
        Require ((Get-FileHash -Algorithm SHA256 -LiteralPath $main).Hash -ceq (Get-FileHash -Algorithm SHA256 -LiteralPath $alias).Hash) 'Alias copy differs.'
        Require (-not ((Get-Item -LiteralPath $alias).Attributes -band [IO.FileAttributes]::ReparsePoint)) 'Alias requires a symlink.'
        $manifest = Get-Content -LiteralPath (Join-Path $bin '.devtools-install.json') -Raw | ConvertFrom-Json
        Require ($manifest.main.index_low -ne $manifest.alias.index_low -or $manifest.main.index_high -ne $manifest.alias.index_high) 'Alias shares the main file identity.'
        # Both invocations replace an image that is currently running.
        $updated = Invoke-Json $alias @('update', '--version', '0.0.0-test.2', '--source', $releases)
        Require ($updated.ok -and $updated.data.version -ceq '0.0.0-test.2' -and $updated.data.alias.status -eq 'unchanged') 'Alias-invoked native update failed.'
        $reported = Invoke-Json $main @('version')
        Require ($reported.data.version -ceq '0.0.0-test.2' -and $reported.data.commit -ceq $Commit) 'Updated main identity mismatch.'
        $reported = Invoke-Json $alias @('version')
        Require ($reported.data.version -ceq '0.0.0-test.2' -and $reported.data.commit -ceq $Commit) 'Updated alias identity mismatch.'
        $updated = Invoke-Json $main @('update', '--version', '0.0.0-test.1', '--source', $releases)
        Require ($updated.ok -and $updated.data.version -ceq '0.0.0-test.1') 'Main-invoked running-image update failed.'
        # No runtime package manager or Unix shell is needed.
        $savedPath = $env:PATH
        try { $env:PATH = ''; $reported = Invoke-Json $alias @('version') } finally { $env:PATH = $savedPath }
        Require ($reported.ok -and $reported.data.version -ceq '0.0.0-test.1') 'Standalone alias requires PATH dependencies.'
        Require (-not [IO.File]::Exists((Join-Path $bin '.devtools-install-journal.json'))) 'Successful update left incomplete journal.'
        Require ([IO.File]::Exists((Join-Path $bin '.devtools-install.lock'))) 'Stable empty install lock was removed.'
        $results += @{ shell = $shell; shell_version = $shellVersion; arch = $Arch; emulated_x64 = ($shell -eq $emulatedShell); install = 'pass'; alias_update = 'pass'; main_update = 'pass'; user_path_unchanged = $true; runtime_path_empty = 'pass' }
    }
    $pathBefore = [Environment]::GetEnvironmentVariable('Path', 'User')
    $pathBin = Join-Path $root 'explicit PATH install/bin'
    try {
        $installed = Invoke-Json $shells[1] @('-NoProfile', '-NonInteractive', '-ExecutionPolicy', 'Bypass', '-File', $installer, '-Version', '0.0.0-test.1', '-Source', $releases, '-BinDir', $pathBin, '-AddToPath')
        Require ($installed.ok) 'Explicit PATH installation failed.'
        $entries = [Environment]::GetEnvironmentVariable('Path', 'User') -split ';'
        Require ([bool]($entries | Where-Object { $_ -ieq $pathBin })) 'Explicit AddToPath was not applied.'
    } finally { [Environment]::SetEnvironmentVariable('Path', $pathBefore, 'User') }
    $results | ConvertTo-Json -Depth 6 | Set-Content -Encoding utf8 windows-install-results.json
} catch {
    $diagnostic = Join-Path $root 'diagnostic.txt'
    if (Test-Path -LiteralPath $diagnostic) { Copy-Item -LiteralPath $diagnostic -Destination windows-install-diagnostic.txt -Force }
    throw
} finally {
    $env:VERSION = $oldVersion; $env:COMMIT = $oldCommit; $env:OUTPUT_DIR = $oldOutput; $env:TARGET_OS = $oldOS; $env:TARGET_ARCH = $oldArch
    if (Test-Path -LiteralPath $root) { Remove-Item -LiteralPath $root -Recurse -Force -ErrorAction SilentlyContinue }
}
