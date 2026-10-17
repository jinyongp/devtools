[CmdletBinding()]
param(
    [string]$Version,
    [string]$Source,
    [string]$BinDir,
    [switch]$AddToPath
)

$ErrorActionPreference = 'Stop'
$stage = $null
$candidateFailure = $null
$previousTls = [Net.ServicePointManager]::SecurityProtocol

function Write-Utf8Json([string]$Text, [bool]$ErrorStream = $false) {
    $bytes = [Text.UTF8Encoding]::new($false).GetBytes($Text.TrimEnd() + "`n")
    if ($ErrorStream) { $stream = [Console]::OpenStandardError() } else { $stream = [Console]::OpenStandardOutput() }
    $stream.Write($bytes, 0, $bytes.Length)
    $stream.Flush()
}

function Read-Artifact([string]$Directory, [string]$Name, [long]$Limit) {
    if ($Directory -match '://') {
        $address = [Uri]($Directory.TrimEnd('/') + '/' + $Name)
        if ($address.Scheme -ne 'https' -or $address.UserInfo -or $address.Fragment) { throw 'HTTPS release directory required.' }
        $handler = [Net.Http.HttpClientHandler]::new()
        $handler.AllowAutoRedirect = $false
        $client = [Net.Http.HttpClient]::new($handler)
        $client.Timeout = [TimeSpan]::FromSeconds(120)
        $deadline = [Threading.CancellationTokenSource]::new([TimeSpan]::FromSeconds(120))
        try {
            for ($redirect = 0; $redirect -le 10; $redirect++) {
                $response = $client.GetAsync($address, [Net.Http.HttpCompletionOption]::ResponseHeadersRead, $deadline.Token).GetAwaiter().GetResult()
                if ([int]$response.StatusCode -in 301, 302, 303, 307, 308) {
                    $next = [Uri]::new($address, $response.Headers.Location)
                    $response.Dispose()
                    if ($next.Scheme -ne 'https' -or $next.UserInfo -or $next.Fragment) { throw 'Unsafe release redirect.' }
                    $address = $next
                    continue
                }
                try {
                    if ([int]$response.StatusCode -ne 200) { throw 'Cannot download release artifact.' }
                    $inputStream = $response.Content.ReadAsStreamAsync().GetAwaiter().GetResult()
                    $memory = [IO.MemoryStream]::new()
                    try {
                        $buffer = [byte[]]::new(65536)
                        while (($count = $inputStream.ReadAsync($buffer, 0, $buffer.Length, $deadline.Token).GetAwaiter().GetResult()) -gt 0) {
                            if ($memory.Length + $count -gt $Limit) { throw 'Release artifact exceeds size limit.' }
                            $memory.Write($buffer, 0, $count)
                        }
                        return ,$memory.ToArray()
                    } finally { $memory.Dispose(); $inputStream.Dispose() }
                } finally { $response.Dispose() }
            }
            throw 'Too many release redirects.'
        } finally { $deadline.Dispose(); $client.Dispose(); $handler.Dispose() }
    }
    $path = [IO.Path]::GetFullPath((Join-Path $Directory $Name))
    $item = Get-Item -LiteralPath $path -Force
    if ($item.PSIsContainer -or ($item.Attributes -band [IO.FileAttributes]::ReparsePoint) -or $item.Length -gt $Limit) { throw 'Regular bounded release artifact required.' }
    return ,[IO.File]::ReadAllBytes($path)
}

function Invoke-Candidate([string]$Executable, [string]$Arguments, [string]$InputText) {
    $start = [Diagnostics.ProcessStartInfo]::new()
    $start.FileName = $Executable
    $start.Arguments = $Arguments
    $start.UseShellExecute = $false
    $start.CreateNoWindow = $true
    $start.RedirectStandardInput = $true
    $start.RedirectStandardOutput = $true
    $start.RedirectStandardError = $true
    $start.StandardOutputEncoding = [Text.UTF8Encoding]::new($false)
    $start.StandardErrorEncoding = [Text.UTF8Encoding]::new($false)
    $process = [Diagnostics.Process]::new()
    $process.StartInfo = $start
    try {
        if (-not $process.Start()) { throw 'Cannot start verified candidate.' }
        $stdout = $process.StandardOutput.ReadToEndAsync()
        $stderr = $process.StandardError.ReadToEndAsync()
        $inputBytes = [Text.UTF8Encoding]::new($false).GetBytes($InputText)
        $process.StandardInput.BaseStream.Write($inputBytes, 0, $inputBytes.Length)
        $process.StandardInput.BaseStream.Flush()
        $process.StandardInput.Close()
        if (-not $process.WaitForExit(180000)) { $process.Kill(); $process.WaitForExit(); throw 'Candidate operation timed out; inspect the installation journal before retrying.' }
        $output = $stdout.GetAwaiter().GetResult()
        $diagnostic = $stderr.GetAwaiter().GetResult()
        if ($process.ExitCode -ne 0 -or $diagnostic -or $output.Length -gt 65536) {
            if ($diagnostic.Length -le 65536 -and $Arguments -eq '__windows-install') {
                try {
                    $failure = $diagnostic | ConvertFrom-Json
                    if ($failure.error.code -eq 'install_failed') { $script:candidateFailure = [string]$failure.error.message }
                } catch {}
            }
            throw 'Verified candidate did not complete installation or version verification.'
        }
        return $output
    } finally { $process.Dispose() }
}

try {
    if ($env:DEVTOOLS_ISOLATED_ROOT) { throw 'Installation is unavailable in isolated mode.' }
    if ($Source -and -not $Version) { throw 'Source requires an explicit Version.' }
    Add-Type -AssemblyName System.Net.Http
    Add-Type -AssemblyName System.IO.Compression
    if (-not ('DevtoolsInstallerNativeArchitecture' -as [type])) {
    Add-Type -TypeDefinition @'
using System;
using System.Runtime.InteropServices;
public static class DevtoolsInstallerNativeArchitecture {
    [DllImport("kernel32.dll", SetLastError = true)]
    public static extern bool IsWow64Process2(IntPtr process, out ushort processMachine, out ushort nativeMachine);
    [DllImport("kernel32.dll")]
    public static extern IntPtr GetCurrentProcess();
}
'@
    }
    [ushort]$processMachine = 0
    [ushort]$nativeMachine = 0
    if (-not [DevtoolsInstallerNativeArchitecture]::IsWow64Process2([DevtoolsInstallerNativeArchitecture]::GetCurrentProcess(), [ref]$processMachine, [ref]$nativeMachine)) { throw 'Cannot resolve native OS architecture.' }
    switch ([int]$nativeMachine) { 34404 { $arch = 'amd64' }; 43620 { $arch = 'arm64' }; default { throw 'Unsupported native architecture.' } }
    [Net.ServicePointManager]::SecurityProtocol = $previousTls -bor [Net.SecurityProtocolType]::Tls12
    if (-not $Source) {
        $releaseBase = $env:DEVTOOLS_RELEASE_URL
        if (-not $releaseBase) { $releaseBase = 'https://github.com/jinyongp/devtools/releases' }
        $uri = [Uri]$releaseBase
        if (-not $uri.IsAbsoluteUri -or $uri.Scheme -ne 'https' -or $uri.UserInfo -or $uri.Query -or $uri.Fragment) { throw 'Release URL must use HTTPS.' }
        if (-not $Version) { $Version = [Text.Encoding]::UTF8.GetString((Read-Artifact ($releaseBase.TrimEnd('/') + '/latest/download') 'version.txt' 256)).Trim() }
        $Source = $releaseBase.TrimEnd('/') + '/download/v' + $Version
    }
    if ($Version -notmatch '^0\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-[0-9A-Za-z]+([.-][0-9A-Za-z]+)*)?$') { throw 'Invalid release Version.' }
    if ($Source -match '://') {
        $uri = [Uri]$Source
        if (-not $uri.IsAbsoluteUri -or $uri.Scheme -ne 'https' -or $uri.UserInfo -or $uri.Query -or $uri.Fragment) { throw 'Release Source must be an HTTPS directory.' }
    }
    $archiveName = 'devtools_' + $Version + '_windows_' + $arch + '.zip'
    $checksumText = [Text.Encoding]::UTF8.GetString((Read-Artifact $Source ($archiveName + '.sha256') 1024)).Trim()
    $fields = $checksumText -split '\s+'
    if ($fields.Count -ne 2 -or $fields[0] -notmatch '^[a-fA-F0-9]{64}$' -or $fields[1] -cne $archiveName) { throw 'Invalid artifact checksum.' }
    $archive = Read-Artifact $Source $archiveName (256MB)
    $sha = [Security.Cryptography.SHA256]::Create()
    try { $actual = [BitConverter]::ToString($sha.ComputeHash($archive)).Replace('-', '').ToLowerInvariant() } finally { $sha.Dispose() }
    if ($actual -cne $fields[0].ToLowerInvariant()) { throw 'Artifact checksum mismatch.' }
    $memory = [IO.MemoryStream]::new($archive, $false)
    $zip = [IO.Compression.ZipArchive]::new($memory, [IO.Compression.ZipArchiveMode]::Read)
    try {
        if ($zip.Entries.Count -ne 1) { throw 'ZIP must contain exactly one executable.' }
        $entry = $zip.Entries[0]
        $unixType = ($entry.ExternalAttributes -shr 16) -band 61440
        if ($entry.FullName -cne 'devtools.exe' -or $entry.Length -le 0 -or $entry.Length -gt 256MB -or $unixType -ne 32768) { throw 'ZIP must contain one regular devtools.exe.' }
        $stream = $entry.Open(); $image = [IO.MemoryStream]::new()
        try { $buffer = [byte[]]::new(65536)
            while (($count = $stream.Read($buffer, 0, $buffer.Length)) -gt 0) {
                if ($image.Length + $count -gt 256MB) { throw 'Expanded executable exceeds size limit.' }
                $image.Write($buffer, 0, $count)
            }
            $body = $image.ToArray() } finally { $stream.Dispose(); $image.Dispose() }
    } finally { $zip.Dispose(); $memory.Dispose() }
    if ($body.Length -lt 64 -or $body[0] -ne 77 -or $body[1] -ne 90) { throw 'Invalid PE executable.' }
    $pe = [BitConverter]::ToUInt32($body, 60)
    if ($pe -gt $body.Length - 26 -or [BitConverter]::ToUInt32($body, $pe) -ne 17744 -or [BitConverter]::ToUInt16($body, $pe + 4) -ne $nativeMachine -or [BitConverter]::ToUInt16($body, $pe + 24) -ne 523) { throw 'PE native architecture mismatch.' }
    $flags = [BitConverter]::ToUInt16($body, $pe + 22)
    if (-not ($flags -band 2) -or ($flags -band 8192)) { throw 'Executable PE image required.' }
    $pattern = 'devtools-build-v1:' + [Regex]::Escape($Version) + ':([A-Za-z0-9._-]{1,128}):windows:' + $arch + ':end'
    $matches = [Regex]::Matches([Text.Encoding]::ASCII.GetString($body), $pattern)
    if ($matches.Count -ne 1) { throw 'Release linked version/commit identity mismatch.' }
    $commit = $matches[0].Groups[1].Value
    # Use a private user-owned staging directory; no installed path is mutated here.
    $stage = Join-Path ([IO.Path]::GetTempPath()) ('devtools-bootstrap-' + [Guid]::NewGuid().ToString('N'))
    $sid = [Security.Principal.WindowsIdentity]::GetCurrent().User
    $security = [Security.AccessControl.DirectorySecurity]::new()
    $security.SetOwner($sid)
    $security.SetAccessRuleProtection($true, $false)
    foreach ($principal in @($sid, [Security.Principal.SecurityIdentifier]::new('S-1-5-18'))) {
        $rule = [Security.AccessControl.FileSystemAccessRule]::new($principal, 'FullControl', 'ContainerInherit,ObjectInherit', 'None', 'Allow')
        $security.AddAccessRule($rule)
    }
    if ($PSVersionTable.PSVersion.Major -ge 6) {
        [IO.FileSystemAclExtensions]::Create([IO.DirectoryInfo]::new($stage), $security) | Out-Null
    } else {
        [IO.Directory]::CreateDirectory($stage, $security) | Out-Null
    }
    $candidate = Join-Path $stage 'devtools.exe'
    [IO.File]::WriteAllBytes($candidate, $body)
    $versionOutput = Invoke-Candidate $candidate 'version' ''
    $reported = $versionOutput | ConvertFrom-Json
    if (-not $reported.ok -or $reported.data.version -cne $Version -or $reported.data.commit -cne $commit) { throw 'Release runtime version/commit mismatch.' }
    $externalAlias = [bool](Get-Command dvt -ErrorAction SilentlyContinue)
    $request = @{ action = 'install'; bin_dir = $BinDir; version = $Version; external_alias = $externalAlias } | ConvertTo-Json -Compress
    $output = Invoke-Candidate $candidate '__windows-install' $request
    $result = $output | ConvertFrom-Json
    if (-not $result.ok -or $result.data.action -ne 'install' -or $result.data.version -cne $Version) { throw 'Installation result mismatch.' }
    if (-not $BinDir) { $BinDir = Join-Path ([Environment]::GetFolderPath('LocalApplicationData')) 'Programs\devtools\bin' }
    $BinDir = [IO.Path]::GetFullPath($BinDir)
    if ($AddToPath) {
        $currentPath = [Environment]::GetEnvironmentVariable('Path', 'User')
        $entries = @($currentPath -split ';' | Where-Object { $_ })
        if (-not ($entries | Where-Object { $_.TrimEnd('\') -ieq $BinDir.TrimEnd('\') })) {
            [Environment]::SetEnvironmentVariable('Path', (($entries + $BinDir) -join ';'), 'User')
        }
    }
    Write-Utf8Json $output
    [Console]::Error.WriteLine('Installed in ' + $BinDir + '. Open a new terminal after adding this directory to your user PATH.')
} catch {
    $message = 'Windows installation failed. Check the selected release, installation ownership/lock and any retained recovery journal before retrying.'
    if ($candidateFailure) { $message = $candidateFailure }
    Write-Utf8Json (@{ schema_version = 1; ok = $false; error = @{ code = 'install_failed'; message = $message } } | ConvertTo-Json -Compress) $true
    exit 1
} finally {
    [Net.ServicePointManager]::SecurityProtocol = $previousTls
    if ($stage -and [IO.Directory]::Exists($stage)) { Remove-Item -LiteralPath $stage -Recurse -Force -ErrorAction SilentlyContinue }
}
