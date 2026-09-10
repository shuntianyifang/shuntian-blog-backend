$ErrorActionPreference='Stop'
$taskRoot=Split-Path $PSScriptRoot -Parent
$taskLocal=Join-Path $taskRoot '.local'
$taskPIDFile=Join-Path $taskLocal 'server.pid'
if(Test-Path $taskPIDFile){throw 'Local API PID file exists; inspect or stop it before starting.'}
$previous=@{}
try{
    foreach($line in Get-Content (Join-Path $taskLocal 'dev.env')){
        if($line -match '^([A-Z_]+)=(.*)$'){$previous[$Matches[1]]=[Environment]::GetEnvironmentVariable($Matches[1],'Process');[Environment]::SetEnvironmentVariable($Matches[1],$Matches[2],'Process')}
    }
    Push-Location $taskRoot
    try{& go build -o .local/blog.exe ./cmd/blog;if($LASTEXITCODE -ne 0){throw 'Go build failed.'}}finally{Pop-Location}
    $process=Start-Process -FilePath (Join-Path $taskLocal 'blog.exe') -ArgumentList 'serve' -WorkingDirectory $taskRoot -WindowStyle Hidden -RedirectStandardOutput (Join-Path $taskLocal 'server.log') -RedirectStandardError (Join-Path $taskLocal 'server-error.log') -PassThru
    $process.Id | Set-Content -LiteralPath $taskPIDFile
    Write-Output "Local API started (PID $($process.Id)); verify /api/health/ready."
}finally{foreach($key in $previous.Keys){[Environment]::SetEnvironmentVariable($key,$previous[$key],'Process')}}
