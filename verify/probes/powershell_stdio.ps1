param(
    [Parameter(Mandatory=$true)][string]$Installer,
    [Parameter(Mandatory=$true)][string]$Binary,
    [Parameter(Mandatory=$true)][string]$Commit,
    [ValidateRange(1, 2000)][int]$Count = 1000
)
$ErrorActionPreference = 'Stop'
$tokens = $null; $issues = $null
$ast = [System.Management.Automation.Language.Parser]::ParseFile($Installer, [ref]$tokens, [ref]$issues)
if ($issues.Count) { throw 'Installer source is not parseable.' }
$function = @($ast.FindAll({ param($node) $node -is [System.Management.Automation.Language.FunctionDefinitionAst] -and $node.Name -eq 'Invoke-Candidate' }, $false))
if ($function.Count -ne 1) { throw 'Installer subprocess function is missing or ambiguous.' }
# Exercise the actual implementation without running the installation flow.
. ([scriptblock]::Create($function[0].Extent.Text))
for ($iteration = 1; $iteration -le $Count; $iteration++) {
    $identity = Invoke-Candidate $Binary 'version' '' | ConvertFrom-Json
    if (-not $identity.ok -or $identity.data.commit -cne $Commit -or $identity.data.version -cne '0.0.0-test.1') {
        throw 'Installer subprocess control returned the wrong candidate identity.'
    }
}
@{ probes = $Count; result = 'pass'; commit = $Commit } | ConvertTo-Json -Compress
