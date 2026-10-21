param(
    [Parameter(Mandatory = $true)][string]$BundleDirectory,
    [Parameter(Mandatory = $true)][string]$ProjectDirectory,
    [Parameter(Mandatory = $true)][ValidateSet('unrestricted', 'appcontainer')][string]$ExpectedIsolation,
    [switch]$MatrixOnly,
    [switch]$CompareCandidate
)
$ErrorActionPreference = 'Stop'
$canSave = $false
$report = [ordered]@{
    format = 'devtools-storage-trace-run-v1'
    started_utc = [DateTime]::UtcNow.ToString('o')
    expected_isolation = $ExpectedIsolation
    project = $ProjectDirectory
    matrix_only = [bool]$MatrixOnly
    checks = @()
    note = 'Trace records contain local paths and file IDs. No stored values or task bodies are saved. CLI queries may create locks or perform normal recovery. Handled storage errors are not necessarily command failures.'
}

function Quote-Argument([string]$value) {
    # Windows CRT quoting, including trailing backslashes and embedded quotes.
    return '"' + [regex]::Replace([regex]::Replace($value, '(\\*)"', '$1$1\"'), '(\\+)$', '$1$1') + '"'
}

function Invoke-Trace([string]$file, [string[]]$arguments, [string]$kind) {
    $start = New-Object Diagnostics.ProcessStartInfo
    $start.FileName = Join-Path $BundleDirectory $file
    $start.Arguments = ($arguments | ForEach-Object { Quote-Argument $_ }) -join ' '
    $start.WorkingDirectory = $ProjectDirectory
    $start.UseShellExecute = $false
    $start.RedirectStandardOutput = $true
    $start.RedirectStandardError = $true
    $process = New-Object Diagnostics.Process
    $process.StartInfo = $start
    $record = [ordered]@{ binary = $file; arguments = $arguments; kind = $kind; started_utc = [DateTime]::UtcNow.ToString('o'); ok = $false; traces = @() }
    try {
        $null = $process.Start()
        $outTask = $process.StandardOutput.ReadToEndAsync()
        $errTask = $process.StandardError.ReadToEndAsync()
        if (-not $process.WaitForExit(60000)) {
            $process.Kill(); $process.WaitForExit()
            $record.timeout = $true
        }
        $stdout = $outTask.GetAwaiter().GetResult()
        $stderr = $errTask.GetAwaiter().GetResult()
        $record.exit_code = $process.ExitCode
        $remaining = @()
        foreach ($line in ($stderr -split '\r?\n')) {
            if ($line.StartsWith('DEVTOOLS_STORAGE_TRACE_V1 ')) {
                $record.traces += ($line.Substring(26) | ConvertFrom-Json)
            } elseif (-not [string]::IsNullOrWhiteSpace($line)) { $remaining += $line }
        }
        if ($kind -eq 'environment') {
            if ($stdout -match 'STORAGE_TRACE_ENV (\{[^\r\n]+\})') { $record.data = $Matches[1] | ConvertFrom-Json }
            $record.ok = ($process.ExitCode -eq 0 -and $null -ne $record.data)
        } elseif ($kind -eq 'matrix') {
            $record.fixture_output = $stdout
            $record.ok = ($process.ExitCode -eq 0 -and -not $record.timeout)
        } else {
            $raw = if ([string]::IsNullOrWhiteSpace($stdout)) { $remaining -join "`n" } else { $stdout }
            try { $response = $raw | ConvertFrom-Json } catch { $record.envelope_parse_failed = $true; return $record }
            $record.ok = ($process.ExitCode -eq 0 -and $response.ok -eq $true -and -not $record.timeout)
            if ($response.ok -eq $false) {
                $record.error = [ordered]@{ code = $response.error.code; message = $response.error.message; details = $response.error.details | Select-Object domain, operation, resource, reason, os_error, os_error_domain }
            } elseif ($kind -eq 'version') { $record.data = $response.data }
            else { $record.data = [ordered]@{ profile = $response.data.profile; revision = $response.data.revision; item_count = $(if ($null -eq $response.data.items) { 0 } else { @($response.data.items).Count }) } }
        }
        return $record
    } finally { $process.Dispose() }
}

function Save-Report {
    $report.recorded_utc = [DateTime]::UtcNow.ToString('o')
    $path = Join-Path $BundleDirectory ('trace-' + $ExpectedIsolation + '-' + [DateTime]::UtcNow.ToString('yyyyMMddTHHmmssZ') + '-' + [guid]::NewGuid().ToString('N') + '.json')
    $stream = [IO.File]::Open($path, [IO.FileMode]::CreateNew, [IO.FileAccess]::Write, [IO.FileShare]::Read)
    $writer = New-Object IO.StreamWriter($stream, (New-Object Text.UTF8Encoding($false)))
    try { $writer.Write(($report | ConvertTo-Json -Depth 24)) } finally { $writer.Dispose() }
    Write-Host "Evidence: $path"
}

try {
    $tmp = [IO.Path]::GetFullPath((Join-Path $ProjectDirectory '.tmp')).TrimEnd('\') + '\'
    if (-not ([IO.Path]::GetFullPath($BundleDirectory)).StartsWith($tmp, [StringComparison]::OrdinalIgnoreCase)) { throw 'Evidence must stay inside the project .tmp directory.' }
    $canSave = $true
    $manifest = Get-Content -LiteralPath (Join-Path $BundleDirectory 'build.json') -Raw | ConvertFrom-Json
    foreach ($artifact in $manifest.artifacts) {
        $actual = (Get-FileHash -LiteralPath (Join-Path $BundleDirectory $artifact.file) -Algorithm SHA256).Hash.ToLowerInvariant()
        if ($actual -ne $artifact.sha256) { throw 'Diagnostic artifact hash mismatch.' }
    }
    $report.build = $manifest
    $environment = Invoke-Trace 'legacy-matrix.exe' @('-test.run=^TestStorageTraceEnvironment$', '-test.v') 'environment'
    $report.checks += $environment
    if (-not $environment.ok -or $environment.data.isolation -ne $ExpectedIsolation) { throw 'Execution environment mismatch; stop without retrying elsewhere.' }
    if ($environment.data.isolated_root_set) { throw 'An isolated root override is set; stop without changing the environment.' }
    if ($MatrixOnly) {
        foreach ($mode in @('legacy', 'candidate')) {
            $matrix = Invoke-Trace "$mode-matrix.exe" @('-test.run=^TestStorageTrace(Matrix|ControlledNegative)$', '-test.v', '-test.timeout=50s', "-storage-matrix-root=$BundleDirectory") 'matrix'
            $control = @($matrix.traces | Where-Object { $_.event -eq 'containment_failure' -and $_.requested_root -match '\\expected$' -and $_.expected_entry_matches_opened_file -eq $false })
            $matrix.capture_contract_ok = ($control.Count -ge 1)
            $matrix.trace_omits_content = -not (($matrix.traces | ConvertTo-Json -Depth 24) -match 'STORAGE_TRACE_CONTENT_CANARY')
            $matrix.ok = ($matrix.ok -and $matrix.capture_contract_ok -and $matrix.trace_omits_content)
            $report.checks += $matrix
        }
        $report.verdict = if (@($report.checks | Where-Object { -not $_.ok }).Count) { 'fixture_matrix_failed' } else { 'fixture_matrix_completed_check_case_results' }
    } else {
        $version = Invoke-Trace 'legacy-cli.exe' @('version') 'version'
        $report.checks += $version
        if (-not $version.ok -or $version.data.commit -ne 'c14191b84475b583d9a480139157fb25ba71c713') { throw 'Legacy diagnostic source identity mismatch.' }
        $legacyFailed = $false
        foreach ($command in @(@('task', 'workstream', 'list', '--profile', 'sample', '--limit', '5'), @('task', 'current', '--dir', '.'))) {
            $check = Invoke-Trace 'legacy-cli.exe' $command 'query'
            $report.checks += $check
            if (-not $check.ok) { $legacyFailed = $true; break }
        }
        $report.verdict = if ($legacyFailed) { 'legacy_failure_captured' } else { 'legacy_failure_not_reproduced' }
        # Persist the failing run before any candidate query can recover state.
        if ($legacyFailed -and $CompareCandidate) {
            Save-Report
            $candidateVersion = Invoke-Trace 'candidate-cli.exe' @('version') 'version'
            $report.checks += $candidateVersion
            if (-not $candidateVersion.ok -or $candidateVersion.data.commit -ne '3ecae453c64a45023f6bf044504875fdbdd0c8ae') { throw 'Candidate source identity mismatch.' }
            $report.checks += Invoke-Trace 'candidate-cli.exe' $command 'query'
            $report.verdict = 'legacy_and_candidate_results_captured_compare_traces'
        }
    }
} catch { $report.fatal = $_.Exception.Message; $report.verdict = 'stopped' }
finally {
    if ($canSave) { Save-Report } else { Write-Error 'No evidence written: bundle is outside the project .tmp directory.' }
}
