param(
    [ValidateSet('dev','test')][string]$Environment = 'dev',
    [Parameter(ValueFromRemainingArguments = $true)][string[]]$CommandArgs = @('serve')
)
$ErrorActionPreference = 'Stop'
$taskRoot = Split-Path $PSScriptRoot -Parent
$taskEnv = Join-Path $taskRoot ".local/$Environment.env"
if (-not (Test-Path $taskEnv)) { throw 'Run scripts/setup-db.ps1 first.' }
$previous = @{}
try {
    foreach ($line in Get-Content $taskEnv) {
        if ($line -match '^([A-Z_]+)=(.*)$') {
            $previous[$Matches[1]] = [Environment]::GetEnvironmentVariable($Matches[1], 'Process')
            [Environment]::SetEnvironmentVariable($Matches[1], $Matches[2], 'Process')
        }
    }
    Push-Location $taskRoot
    try { & go run ./cmd/blog @CommandArgs; $taskExit = $LASTEXITCODE } finally { Pop-Location }
} finally {
    foreach ($key in $previous.Keys) { [Environment]::SetEnvironmentVariable($key, $previous[$key], 'Process') }
}
exit $taskExit
