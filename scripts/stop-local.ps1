$ErrorActionPreference='Stop'
$taskRoot=Split-Path $PSScriptRoot -Parent
$taskPIDFile=Join-Path $taskRoot '.local/server.pid'
if(-not(Test-Path $taskPIDFile)){throw 'No local API PID file.'}
$taskID=[int](Get-Content $taskPIDFile -Raw)
$process=Get-Process -Id $taskID -ErrorAction SilentlyContinue
if($process){
    $expected=[IO.Path]::GetFullPath((Join-Path $taskRoot '.local/blog.exe'))
    if($process.Path -ne $expected){throw 'PID belongs to another executable; refusing to stop it.'}
    Stop-Process -Id $taskID
}
Remove-Item -LiteralPath $taskPIDFile
Write-Output 'Local API stopped.'
