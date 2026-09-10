$ErrorActionPreference = 'Stop'
$taskRoot = Split-Path $PSScriptRoot -Parent
$taskConfig = Join-Path $taskRoot '.local/test.env'
if (-not (Test-Path $taskConfig)) { throw 'Test database is required. Run scripts/setup-db.ps1.' }
$taskValues = @{}
foreach ($line in Get-Content $taskConfig) { if ($line -match '^([A-Z_]+)=(.*)$') { $taskValues[$Matches[1]]=$Matches[2] } }
$taskPasswordFile = Join-Path $taskRoot '.local/test-admin-password'
if (-not (Test-Path $taskPasswordFile)) {
    $bytes=New-Object byte[] 24; $rng=[Security.Cryptography.RandomNumberGenerator]::Create()
    try { $rng.GetBytes($bytes) } finally { $rng.Dispose() }
    [Convert]::ToBase64String($bytes) | Set-Content $taskPasswordFile
}
$taskNames = @('TEST_DATABASE_URL','TEST_MIGRATION_DATABASE_URL','TEST_ADMIN_PASSWORD')
$previous=@{}; foreach($name in $taskNames){$previous[$name]=[Environment]::GetEnvironmentVariable($name,'Process')}
try {
    $env:TEST_DATABASE_URL=$taskValues['DATABASE_URL']
    $env:TEST_MIGRATION_DATABASE_URL=$taskValues['MIGRATION_DATABASE_URL']
    $env:TEST_ADMIN_PASSWORD=(Get-Content $taskPasswordFile -Raw).Trim()
    Push-Location $taskRoot
    try { & go test ./... -count=1 -v; $taskExit=$LASTEXITCODE } finally { Pop-Location }
} finally {foreach($name in $taskNames){[Environment]::SetEnvironmentVariable($name,$previous[$name],'Process')}}
exit $taskExit
