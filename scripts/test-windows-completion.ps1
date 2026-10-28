param([Parameter(Mandatory=$true)][string]$Binary, [switch]$Child)
$ErrorActionPreference = 'Stop'
function Require([bool]$Condition,[string]$Message) { if (-not $Condition) { throw $Message } }
if (-not $Child) {
    $scriptCopy = Join-Path $env:RUNNER_TEMP ('completion-smoke-' + [Guid]::NewGuid().ToString('N') + '.ps1')
    [IO.File]::WriteAllText($scriptCopy, [IO.File]::ReadAllText($PSCommandPath), [Text.UTF8Encoding]::new($true))
    try {
        $results = @()
        foreach ($shell in @((Get-Command powershell.exe).Source,(Get-Command pwsh.exe).Source)) {
            $result = & $shell -NoProfile -NonInteractive -ExecutionPolicy Bypass -File $scriptCopy -Binary $Binary -Child
            Require ($LASTEXITCODE -eq 0) ('Completion smoke failed: ' + $shell)
            $results += ($result -join "`n" | ConvertFrom-Json)
        }
        $results | ConvertTo-Json -Depth 6 | Set-Content -Encoding utf8 windows-completion-results.json
    } finally { Remove-Item -LiteralPath $scriptCopy -Force }
    exit 0
}
$root = Join-Path ([IO.Path]::GetTempPath()) ('devtools-completion-' + [Guid]::NewGuid().ToString('N'))
$beforeRoot = $env:DEVTOOLS_ISOLATED_ROOT; $beforePath = $env:PATH
New-Item -ItemType Directory -Path $root | Out-Null
$location = Get-Location
try {
    $bin = Join-Path $root 'bin'; New-Item -ItemType Directory -Path $bin | Out-Null
    Copy-Item -LiteralPath $Binary -Destination (Join-Path $bin 'devtools.exe')
    Copy-Item -LiteralPath $Binary -Destination (Join-Path $bin 'dvt.exe')
    $env:PATH = $bin + ';' + $env:PATH
    $env:DEVTOOLS_ISOLATED_ROOT = Join-Path $root 'isolation'
    Set-Location -LiteralPath $root
    $config = @'
profile = 'completion-fixture'
[commands.web]
exec = ['cmd.exe', '/d', '/c', 'echo CANARY_NEVER_RUN']
[commands."한글 작업's"]
exec = ['cmd.exe', '/d', '/c', 'echo CANARY_NEVER_RUN']
[commands."docs:dev"]
exec = ['cmd.exe', '/d', '/c', 'echo CANARY_NEVER_RUN']
'@
    [IO.File]::WriteAllText((Join-Path $root 'devtools.toml'),$config,[Text.UTF8Encoding]::new($false))
    $main = Join-Path $bin 'devtools.exe'
    $version = & $main --format json version | ConvertFrom-Json
    Require ($LASTEXITCODE -eq 0 -and $version.data.protocol_version -eq 18 -and $version.schema_version -eq 1) 'Version/envelope mismatch'
    $null = & $main --format json var set SAFE_NAME --value CANARY_VALUE
    Require ($LASTEXITCODE -eq 0) 'Public Windows value command failed'
    $diagnostics = & $main --format json diagnostics | ConvertFrom-Json
    Require ($LASTEXITCODE -eq 0 -and $diagnostics.data.platform.os -eq 'windows' -and $diagnostics.data.ready) 'Native diagnostics/platform unavailable'
    $profiles = & $main --format json profile list | ConvertFrom-Json
    Require ($LASTEXITCODE -eq 0 -and [bool]($profiles.data.items | Where-Object { $_.profile -eq 'completion-fixture' -and $_.values })) 'Native profile catalog unavailable'
    $completion = (& $main completion powershell) -join "`n"
    Require ($LASTEXITCODE -eq 0 -and $completion.Contains('Register-ArgumentCompleter -Native')) 'Completion generation failed'
    $completionFile = Join-Path $root 'completion.ps1'
    [IO.File]::WriteAllText($completionFile,$completion,[Text.UTF8Encoding]::new($true))
    . $completionFile
    function Matches([string]$Line) {
        return @([Management.Automation.CommandCompletion]::CompleteInput($Line,$Line.Length,$null).CompletionMatches)
    }
    foreach ($entry in @(
        @{line='devtools ';want='var'},
        @{line='devtools --format ';want='json'},
        @{line='devtools --format=t';want='--format=text'},
        @{line='devtools --format json command ';want='list'},
        @{line='dvt var ';want='get'},
        @{line='devtools task workstream ';want='create'},
        @{line='devtools run --env local ';want='--profile'},
        @{line='devtools run ';want='web'},
        @{line='devtools command explain ';want='web'},
        @{line=("& '" + $main.Replace("'", "''") + "' command explain ");want='web'},
        @{line='devtools command update ';want='web'},
        @{line='devtools command rename ';want='web'},
        @{line='devtools run --output=r';want='--output=redacted'},
        @{line='devtools doctor --mode ';want='managed'},
        @{line='devtools task list --state ';want='open'},
        @{line='devtools run 한';want="한글 작업's"},
        @{line="devtools run '한";want="한글 작업's"},
        @{line='devtools run docs:';want='docs:dev'},
        @{line='devtools var get ';want='SAFE_NAME'},
        @{line='devtools completion ';want='powershell'}
    )) {
        $matches = @(Matches $entry.line)
        Require ([bool]($matches | Where-Object { $_.ListItemText -ceq $entry.want })) ('Missing completion for ' + $entry.line)
        foreach ($match in $matches) {
            if ($match.ListItemText -ceq "한글 작업's") { Require ($match.CompletionText -ceq "'한글 작업''s'") 'PowerShell quote escaping failed' }
        }
    }
    foreach ($line in @('devtools run web --env ','devtools command run web --profile ','devtools run -- ')) {
        $matches = @(Matches $line)
        Require (-not [bool]($matches | Where-Object { $_.ListItemText -in @('--profile','--env','completion-fixture') })) 'Child argv boundary leaked devtools candidates'
    }
    $midLine = 'devtools run docs:dev --env child'
    $midCursor = 'devtools run docs:'.Length
    $midMatches = @([Management.Automation.CommandCompletion]::CompleteInput($midLine,$midCursor,$null).CompletionMatches)
    Require ([bool]($midMatches | Where-Object { $_.ListItemText -ceq 'docs:dev' })) 'Cursor prefix included later argv'
    $canary = Join-Path $root 'MUST_NOT_EXIST'
    foreach ($line in @('devtools run $(Set-Content -LiteralPath MUST_NOT_EXIST -Value bad) ', 'devtools run "$(Set-Content -LiteralPath MUST_NOT_EXIST -Value bad)" ', 'devtools var get $env:USERPROFILE ')) {
        $null = Matches $line
        Require (-not (Test-Path -LiteralPath $canary)) 'Completion evaluated a dynamic expression'
    }
    $schema = & $main schema completion | ConvertFrom-Json
    Require ($schema.data.protocol_version -eq 18 -and $schema.data.input_schema.properties.args.prefixItems[0].pattern.Contains('powershell')) 'Completion schema mismatch'
    [pscustomobject]@{shell=$PSVersionTable.PSVersion.ToString();edition=$PSEdition;protocol=18;native_registration='pass';utf8_nul_dynamic_names='pass';single_quote='pass';child_boundary='pass';dynamic_expression_canary='pass';platform_schema='pass'} | ConvertTo-Json
} finally {
    Set-Location -LiteralPath $location.Path
    $env:DEVTOOLS_ISOLATED_ROOT = $beforeRoot; $env:PATH = $beforePath
    Remove-Item -LiteralPath $root -Recurse -Force -ErrorAction SilentlyContinue
}
