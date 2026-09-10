$ErrorActionPreference='Stop'
$taskRoot=Split-Path $PSScriptRoot -Parent
$taskFile=Join-Path $taskRoot '.local/demo-admin.json'
if(Test-Path $taskFile){Write-Output 'Demo administrator configuration already exists; unchanged.';exit 0}
$bytes=New-Object byte[] 24; $rng=[Security.Cryptography.RandomNumberGenerator]::Create()
try{$rng.GetBytes($bytes)}finally{$rng.Dispose()}
$taskSecret=[Convert]::ToBase64String($bytes)
$previous=$env:ADMIN_PASSWORD
try{
    $env:ADMIN_PASSWORD=$taskSecret
    & (Join-Path $PSScriptRoot 'run.ps1') dev admin local-admin
    if($LASTEXITCODE -ne 0){throw 'Admin creation failed; no credentials file was written. Existing administrator is not replaced.'}
    @{username='local-admin';password=$taskSecret} | ConvertTo-Json | Set-Content -LiteralPath $taskFile -Encoding utf8
    Write-Output 'Random local administrator credentials saved in .local/demo-admin.json (not printed).'
}finally{$env:ADMIN_PASSWORD=$previous}
