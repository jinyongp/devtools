param(
    [Parameter(Mandatory=$true)][string]$Arch,
    [Parameter(Mandatory=$true)][string]$Commit,
    [ValidateSet('all', 'ps5', 'ps7', 'x64')][string]$Engine = 'all',
    [ValidateRange(1, 20)][int]$Repeat = 1,
    [ValidateRange(0, 1000)][int]$StartupProbes = 0
)
$ErrorActionPreference = 'Stop'
$temporaryBase = $env:RUNNER_TEMP
if (-not $temporaryBase) { $temporaryBase = [IO.Path]::GetTempPath() }
$root = Join-Path $temporaryBase ('devtools-install-check-' + [Guid]::NewGuid().ToString('N'))
New-Item -ItemType Directory -Path $root | Out-Null
$releases = Join-Path $root 'release files 한글'
$installer = Join-Path $PWD 'scripts/install.ps1'
$oldVersion = $env:VERSION; $oldCommit = $env:COMMIT; $oldOutput = $env:OUTPUT_DIR; $oldOS = $env:TARGET_OS; $oldArch = $env:TARGET_ARCH
$originalUserPath = [Environment]::GetEnvironmentVariable('Path', 'User')
$originalMachinePath = [Environment]::GetEnvironmentVariable('Path', 'Machine')
$oldInstallerFixture = $env:DEVTOOLS_INSTALLER_FIXTURE
$bootstrapBin = Join-Path ([Environment]::GetFolderPath('LocalApplicationData')) 'Programs\devtools\bin'
$bootstrapOwned = $false
function Require([bool]$Condition, [string]$Message) { if (-not $Condition) { throw $Message } }
function Save-InvocationFailure([string]$Executable, [string[]]$Arguments, [int]$ExitCode, [datetime]$Started, [string]$Output) {
    $caller = Get-PSCallStack | Select-Object -Skip 2 -First 1
    $diagnostic = Join-Path $root 'diagnostic.txt'
    $bytes = if (Test-Path -LiteralPath $diagnostic) { (Get-Item -LiteralPath $diagnostic).Length } else { 0 }
    $context = [ordered]@{
        engine = [IO.Path]::GetFileName($Executable)
        exit_code = $ExitCode
        exit_hex = ('0x{0:X8}' -f $ExitCode)
        started = $Started.ToUniversalTime().ToString('o')
        script_line = $caller.ScriptLineNumber
        iteration = $iteration
        startup_probe = $probe
        argument_flags = @($Arguments | Where-Object { $_ -match '^--?[A-Za-z][A-Za-z0-9-]*$' } | Select-Object -First 16)
        stderr_bytes = $bytes
    }
    [IO.File]::WriteAllText((Join-Path $PWD 'windows-install-context.json'), ($context | ConvertTo-Json -Depth 3), [Text.UTF8Encoding]::new($false))
    [IO.File]::WriteAllText((Join-Path $PWD 'windows-install-stdout.txt'), $Output, [Text.UTF8Encoding]::new($false))
    try {
        $events = @(Get-WinEvent -FilterHashtable @{ LogName = 'Application'; Id = @(1000, 1001, 1026); StartTime = $Started } -ErrorAction Stop |
            Select-Object -First 20 TimeCreated, Id, ProviderName, Message)
        ConvertTo-Json -InputObject $events -Depth 4 | Set-Content -Encoding utf8 windows-install-events.json
    } catch {
        $_.Exception.Message | Set-Content -Encoding utf8 windows-install-event-error.txt
    }
}
function Invoke-Json([string]$Executable, [string[]]$Arguments) {
    $started = Get-Date
    $text = & $Executable @Arguments 2> (Join-Path $root 'diagnostic.txt')
    $exitCode = $LASTEXITCODE
    if ($exitCode -ne 0) { Save-InvocationFailure $Executable $Arguments $exitCode $started ($text -join "`n") }
    Require ($exitCode -eq 0) ('Command failed; see disposable diagnostic file: ' + $Executable)
    if ($Arguments -contains '-Json') {
        Require (-not (Get-Content -LiteralPath (Join-Path $root 'diagnostic.txt') -Raw)) 'Successful JSON installer wrote human text to stderr.'
    }
    return ($text -join "`n" | ConvertFrom-Json)
}
function Invoke-Text([string]$Executable, [string[]]$Arguments) {
    $started = Get-Date
    $text = & $Executable @Arguments 2> (Join-Path $root 'diagnostic.txt')
    $exitCode = $LASTEXITCODE
    if ($exitCode -ne 0) { Save-InvocationFailure $Executable $Arguments $exitCode $started ($text -join "`n") }
    Require ($exitCode -eq 0) ('Human installer failed; see disposable diagnostic file: ' + $Executable)
    Require (-not (Get-Content -LiteralPath (Join-Path $root 'diagnostic.txt') -Raw)) 'Successful human installer wrote its guidance to stderr.'
    return ($text -join "`n")
}
function Require-HumanGuidance([string]$Text, [string]$Directory) {
    Require ($Text -notmatch '"schema_version"\s*:') 'Default installer exposed a JSON envelope.'
    Require ($Text.Contains($Directory)) 'Installer guidance omitted the actual install directory.'
    Require ($Text.Contains('--help')) 'Installer guidance omitted an executable next step.'
}
try {
    $env:OUTPUT_DIR = $releases; $env:COMMIT = $Commit; $env:TARGET_OS = 'windows'; $env:TARGET_ARCH = $Arch
    foreach ($version in @('0.0.0-test.1', '0.0.0-test.2')) {
        $env:VERSION = $version
        & python -B scripts/package.py
        Require ($LASTEXITCODE -eq 0) 'Cannot prepare native install release.'
    }
    $shells = @()
    if ($Engine -in @('all', 'ps5')) { $shells += (Get-Command powershell.exe).Source }
    if ($Engine -in @('all', 'ps7')) { $shells += (Get-Command pwsh.exe).Source }
    if ($Engine -eq 'x64' -and $Arch -ne 'arm64') { throw 'x64 emulation requires an ARM64 runner.' }
    $emulatedShell = $null
    if ($Arch -eq 'arm64' -and $Engine -in @('all', 'x64')) {
        # Pinned official portable test fixture; no system installation or PATH change.
        $archive = Join-Path $root 'PowerShell-7.6.6-win-x64.zip'
        Invoke-WebRequest -UseBasicParsing -Uri 'https://github.com/PowerShell/PowerShell/releases/download/v7.6.6/PowerShell-7.6.6-win-x64.zip' -OutFile $archive -TimeoutSec 120
        Require ((Get-FileHash -LiteralPath $archive -Algorithm SHA256).Hash.ToLowerInvariant() -ceq '02fe458be20493fbdf43f61ea20610b811ee6c738ab1676c61b9cfcd1a33c860') 'Official x64 PowerShell fixture checksum mismatch.'
        $portable = Join-Path $root 'powershell-x64'
        Expand-Archive -LiteralPath $archive -DestinationPath $portable
        $emulatedShell = Join-Path $portable 'pwsh.exe'
        $actual = Invoke-Json $emulatedShell @('-NoProfile', '-NonInteractive', '-Command', '[pscustomobject]@{process=[Runtime.InteropServices.RuntimeInformation]::ProcessArchitecture.ToString();os=[Runtime.InteropServices.RuntimeInformation]::OSArchitecture.ToString();framework=[Runtime.InteropServices.RuntimeInformation]::FrameworkDescription;version=$PSVersionTable.PSVersion.ToString()} | ConvertTo-Json')
        Require ($actual.process -eq 'X64' -and $actual.os -eq 'Arm64') 'ARM64 fixture did not run actual x64 emulation.'
        $actual | ConvertTo-Json | Set-Content -Encoding utf8 windows-install-emulated-runtime.json
        $shells += $emulatedShell
    }
    if ($StartupProbes -gt 0) {
        $startupResults = @()
        foreach ($shell in $shells) {
            for ($probe = 1; $probe -le $StartupProbes; $probe++) {
                # No installer or devtools process runs in this control.
                $control = Invoke-Json $shell @('-NoProfile', '-NonInteractive', '-Command', '[pscustomobject]@{value=[Math]::Sqrt(144);version=$PSVersionTable.PSVersion.ToString()} | ConvertTo-Json -Compress')
                Require ($control.value -eq 12 -and $control.version) 'Minimal PowerShell startup control failed.'
                if ($probe % 25 -eq 0) { Write-Output ('PowerShell startup controls: ' + $probe + '/' + $StartupProbes + ' (' + $shell + ')') }
            }
            $startupResults += @{ shell = $shell; probes = $StartupProbes; result = 'pass' }
        }
        $probe = $null
        ConvertTo-Json -InputObject $startupResults -Depth 3 | Set-Content -Encoding utf8 windows-powershell-startup-results.json
    }
    $results = @()
    Require (-not (Test-Path -LiteralPath $bootstrapBin)) 'Inline bootstrap fixture requires an unused default installation directory.'
    # Serve the candidate installer text at the documented download boundary.
    # Artifact metadata and packages still come from the public latest stable release.
    $env:DEVTOOLS_INSTALLER_FIXTURE = $installer
    $bootstrapCommand = @'
$ErrorActionPreference = 'Stop'
function Invoke-RestMethod([string]$Uri) {
    if ($Uri -cne 'https://jinyongp.dev/devtools/install.ps1') { throw 'Unexpected bootstrap URL.' }
    return [IO.File]::ReadAllText($env:DEVTOOLS_INSTALLER_FIXTURE)
}
irm https://jinyongp.dev/devtools/install.ps1 | iex
'@
    foreach ($iteration in 1..$Repeat) {
      foreach ($shell in $shells) {
        Write-Output ('Testing installer engine: ' + $shell + ' (iteration ' + $iteration + '/' + $Repeat + ')')
        $shellVersion = Invoke-Json $shell @('-NoProfile', '-NonInteractive', '-Command', '$PSVersionTable.PSVersion.ToString() | ConvertTo-Json')
        $bin = Join-Path $root ('설치 경로 ' + [Guid]::NewGuid().ToString('N') + '/bin')
        $beforePath = [Environment]::GetEnvironmentVariable('Path', 'User')
        $beforeProcessPath = $env:PATH
        $installed = Invoke-Json $shell @('-NoProfile', '-NonInteractive', '-ExecutionPolicy', 'Bypass', '-File', $installer, '-Json', '-Version', '0.0.0-test.1', '-Source', $releases, '-BinDir', $bin)
        Require ($installed.ok -and $installed.data.action -eq 'install') 'Bootstrap install response mismatch.'
        $registeredPath = [Environment]::GetEnvironmentVariable('Path', 'User')
        $entries = @($registeredPath -split ';' | Where-Object { $_ })
        Require (@($entries | Where-Object { $_.TrimEnd('\') -ieq $bin.TrimEnd('\') }).Count -eq 1) 'Default installation did not add its directory exactly once.'
        foreach ($entry in @($beforePath -split ';' | Where-Object { $_ })) {
            Require ($entries -ccontains $entry) 'Default installation removed an existing user PATH entry.'
        }
        $registeredBin = Join-Path $root ('already on PATH ' + [Guid]::NewGuid().ToString('N'))
        $registeredPath += ';' + $registeredBin + '\'
        [Environment]::SetEnvironmentVariable('Path', $registeredPath, 'User')
        $registered = Invoke-Json $shell @('-NoProfile', '-NonInteractive', '-ExecutionPolicy', 'Bypass', '-File', $installer, '-Json', '-Version', '0.0.0-test.1', '-Source', $releases, '-BinDir', $registeredBin)
        Require ($registered.ok) 'Installation into an already registered PATH directory failed.'
        Require ([Environment]::GetEnvironmentVariable('Path', 'User') -ceq $registeredPath) 'Installation duplicated or rewrote an existing user PATH entry.'
        $optOutBin = Join-Path $root ('no PATH 한글 ' + [Guid]::NewGuid().ToString('N'))
        $optOut = Invoke-Text $shell @('-NoProfile', '-NonInteractive', '-ExecutionPolicy', 'Bypass', '-File', $installer, '-Version', '0.0.0-test.1', '-Source', $releases, '-BinDir', $optOutBin, '-AddToPath', '-NoPath')
        Require-HumanGuidance $optOut $optOutBin
        Require ($optOut.Contains((Join-Path $optOutBin 'devtools.exe'))) 'NoPath guidance omitted the direct executable path.'
        Require ([Environment]::GetEnvironmentVariable('Path', 'User') -ceq $registeredPath) 'NoPath changed user PATH or did not override AddToPath.'
        Require ($env:PATH -ceq $beforeProcessPath) 'Installer changed the parent process PATH.'
        Require ([Environment]::GetEnvironmentVariable('Path', 'Machine') -ceq $originalMachinePath) 'Installer changed system PATH.'
        $failureOutput = & $shell '-NoProfile' '-NonInteractive' '-ExecutionPolicy' 'Bypass' '-File' $installer '-Version' 'invalid' '-Source' $releases '-Json' 2> (Join-Path $root 'diagnostic.txt')
        Require ($LASTEXITCODE -ne 0) 'Invalid JSON-mode installation exited successfully.'
        Require (-not ($failureOutput -join '').Trim()) 'JSON-mode failure wrote progress to stdout.'
        $failure = Get-Content -LiteralPath (Join-Path $root 'diagnostic.txt') -Raw | ConvertFrom-Json
        Require ($failure.schema_version -eq 1 -and -not $failure.ok -and $failure.error.code -eq 'install_failed') 'JSON-mode error response mismatch.'
        $humanFailureOutput = & $shell '-NoProfile' '-NonInteractive' '-ExecutionPolicy' 'Bypass' '-File' $installer '-Version' 'invalid' '-Source' $releases 2> (Join-Path $root 'diagnostic.txt')
        Require ($LASTEXITCODE -ne 0) 'Invalid human-mode installation exited successfully.'
        $humanFailure = Get-Content -LiteralPath (Join-Path $root 'diagnostic.txt') -Raw
        Require ($humanFailure -and $humanFailure -notmatch '"schema_version"\s*:') 'Human failure was missing or exposed JSON.'
        $main = Join-Path $bin 'devtools.exe'; $alias = Join-Path $bin 'dvt.exe'
        Require ([IO.File]::Exists($main) -and [IO.File]::Exists($alias)) 'Installer did not publish both executable copies.'
        Require ((Get-FileHash -Algorithm SHA256 -LiteralPath $main).Hash -ceq (Get-FileHash -Algorithm SHA256 -LiteralPath $alias).Hash) 'Alias copy differs.'
        Require (-not ((Get-Item -LiteralPath $alias).Attributes -band [IO.FileAttributes]::ReparsePoint)) 'Alias requires a symlink.'
        $manifest = Get-Content -LiteralPath (Join-Path $bin '.devtools-install.json') -Raw | ConvertFrom-Json
        Require ($manifest.main.index_low -ne $manifest.alias.index_low -or $manifest.main.index_high -ne $manifest.alias.index_high) 'Alias shares the main file identity.'
        $bootstrapUpdate = Invoke-Text $shell @('-NoProfile', '-NonInteractive', '-ExecutionPolicy', 'Bypass', '-File', $installer, '-Version', '0.0.0-test.2', '-Source', $releases, '-BinDir', $bin)
        Require-HumanGuidance $bootstrapUpdate $bin
        Require ($bootstrapUpdate.Contains('Updated devtools ')) 'Repeated installer did not explain the update.'
        $reported = Invoke-Json $main @('version')
        Require ($reported.data.version -ceq '0.0.0-test.2' -and $reported.data.commit -ceq $Commit) 'Repeated installer did not update the installed executable.'
        $repeated = Invoke-Json $shell @('-NoProfile', '-NonInteractive', '-ExecutionPolicy', 'Bypass', '-File', $installer, '-Json', '-Version', '0.0.0-test.2', '-Source', $releases, '-BinDir', $bin)
        Require ($repeated.ok -and $repeated.data.action -eq 'update' -and $repeated.data.version -ceq '0.0.0-test.2' -and $repeated.data.alias.status -eq 'unchanged') 'Same-version installer rerun failed or changed its owned alias.'
        Require ([Environment]::GetEnvironmentVariable('Path', 'User') -ceq $registeredPath) 'Repeated installer duplicated or rewrote user PATH.'
        Require ((Get-FileHash -Algorithm SHA256 -LiteralPath $main).Hash -ceq (Get-FileHash -Algorithm SHA256 -LiteralPath $alias).Hash) 'Repeated installer left different main and alias versions.'
        # Both invocations replace an image that is currently running.
        $updated = Invoke-Json $alias @('update', '--version', '0.0.0-test.1', '--source', $releases)
        Require ($updated.ok -and $updated.data.version -ceq '0.0.0-test.1' -and $updated.data.alias.status -eq 'unchanged') 'Alias-invoked native update failed.'
        $reported = Invoke-Json $main @('version')
        Require ($reported.data.version -ceq '0.0.0-test.1' -and $reported.data.commit -ceq $Commit) 'Updated main identity mismatch.'
        $reported = Invoke-Json $alias @('version')
        Require ($reported.data.version -ceq '0.0.0-test.1' -and $reported.data.commit -ceq $Commit) 'Updated alias identity mismatch.'
        $updated = Invoke-Json $main @('update', '--version', '0.0.0-test.2', '--source', $releases)
        Require ($updated.ok -and $updated.data.version -ceq '0.0.0-test.2') 'Main-invoked running-image update failed.'
        # No runtime package manager or Unix shell is needed.
        $savedPath = $env:PATH
        try { $env:PATH = ''; $reported = Invoke-Json $alias @('version') } finally { $env:PATH = $savedPath }
        Require ($reported.ok -and $reported.data.version -ceq '0.0.0-test.2') 'Standalone alias requires PATH dependencies.'
        Require (-not [IO.File]::Exists((Join-Path $bin '.devtools-install-journal.json'))) 'Successful update left incomplete journal.'
        Require ([IO.File]::Exists((Join-Path $bin '.devtools-install.lock'))) 'Stable empty install lock was removed.'
        $bootstrapOwned = $true
        $inline = Invoke-Text $shell @('-NoProfile', '-NonInteractive', '-Command', $bootstrapCommand)
        Require-HumanGuidance $inline $bootstrapBin
        $inlineVersion = Invoke-Json (Join-Path $bootstrapBin 'devtools.exe') @('version')
        Require ($inlineVersion.ok -and $inline.Contains($inlineVersion.data.version)) 'Human bootstrap did not describe the installed release.'
        # Upgrade an actual public stable installation with the candidate engine.
        $legacyUpdate = Invoke-Text $shell @('-NoProfile', '-NonInteractive', '-ExecutionPolicy', 'Bypass', '-File', $installer, '-Version', '0.0.0-test.2', '-Source', $releases)
        Require-HumanGuidance $legacyUpdate $bootstrapBin
        Require ($legacyUpdate.Contains('Updated devtools ')) 'Installer rejected or misreported an existing public stable installation.'
        $legacyVersion = Invoke-Json (Join-Path $bootstrapBin 'devtools.exe') @('version')
        Require ($legacyVersion.data.version -ceq '0.0.0-test.2' -and $legacyVersion.data.commit -ceq $Commit) 'Installer did not replace the public stable installation with the candidate.'
        $inlineEntries = [Environment]::GetEnvironmentVariable('Path', 'User') -split ';'
        Require (@($inlineEntries | Where-Object { $_.TrimEnd('\') -ieq $bootstrapBin.TrimEnd('\') }).Count -eq 1) 'Inline bootstrap did not register its default directory.'
        Remove-Item -LiteralPath $bootstrapBin -Recurse -Force
        $bootstrapOwned = $false
        [Environment]::SetEnvironmentVariable('Path', $beforePath, 'User')
        $results += @{ shell = $shell; shell_version = $shellVersion; arch = $Arch; emulated_x64 = ($shell -eq $emulatedShell); install = 'pass'; alias_update = 'pass'; main_update = 'pass'; default_user_path = 'pass'; path_idempotency = 'pass'; no_path_opt_out = 'pass'; inline_bootstrap = 'pass'; inline_release = $inlineVersion.data.version; human_output = $inline; no_path_output = $optOut; human_error = 'pass'; json_error = 'pass'; json_success = 'pass'; human_failure_output = $humanFailure; inline_installer_source = 'candidate download fixture'; inline_artifacts = 'public latest stable'; runtime_path_empty = 'pass'; bootstrap_update = 'pass'; same_version_reinstall = 'pass'; legacy_install_update = 'pass'; bootstrap_update_output = $bootstrapUpdate; legacy_update_output = $legacyUpdate }
      }
    }
    $pathBefore = [Environment]::GetEnvironmentVariable('Path', 'User')
    $pathBin = Join-Path $root 'explicit PATH install/bin'
    try {
        $pathShell = if ($Engine -eq 'all') { $shells[1] } else { $shells[0] }
        $installed = Invoke-Json $pathShell @('-NoProfile', '-NonInteractive', '-ExecutionPolicy', 'Bypass', '-File', $installer, '-Json', '-Version', '0.0.0-test.1', '-Source', $releases, '-BinDir', $pathBin, '-AddToPath')
        Require ($installed.ok) 'Explicit PATH installation failed.'
        $entries = [Environment]::GetEnvironmentVariable('Path', 'User') -split ';'
        Require ([bool]($entries | Where-Object { $_ -ieq $pathBin })) 'Explicit AddToPath was not applied.'
    } finally { [Environment]::SetEnvironmentVariable('Path', $pathBefore, 'User') }
    ConvertTo-Json -InputObject @($results) -Depth 6 | Set-Content -Encoding utf8 windows-install-results.json
} catch {
    $diagnostic = Join-Path $root 'diagnostic.txt'
    if (Test-Path -LiteralPath $diagnostic) { Copy-Item -LiteralPath $diagnostic -Destination windows-install-diagnostic.txt -Force }
    throw
} finally {
    [Environment]::SetEnvironmentVariable('Path', $originalUserPath, 'User')
    $env:DEVTOOLS_INSTALLER_FIXTURE = $oldInstallerFixture
    if ($bootstrapOwned -and (Test-Path -LiteralPath $bootstrapBin)) { Remove-Item -LiteralPath $bootstrapBin -Recurse -Force -ErrorAction SilentlyContinue }
    $env:VERSION = $oldVersion; $env:COMMIT = $oldCommit; $env:OUTPUT_DIR = $oldOutput; $env:TARGET_OS = $oldOS; $env:TARGET_ARCH = $oldArch
    if (Test-Path -LiteralPath $root) { Remove-Item -LiteralPath $root -Recurse -Force -ErrorAction SilentlyContinue }
}
