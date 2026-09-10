param(
    [string]$Psql = 'D:\PostgreSQL\18\bin\psql.exe',
    [string]$AdminUser = 'postgres'
)
$ErrorActionPreference = 'Stop'
$taskRoot = Split-Path $PSScriptRoot -Parent
$taskLocal = Join-Path $taskRoot '.local'
New-Item -ItemType Directory -Force $taskLocal | Out-Null
function New-Secret {
    $bytes = New-Object byte[] 32
    $rng = [Security.Cryptography.RandomNumberGenerator]::Create()
    try { $rng.GetBytes($bytes) } finally { $rng.Dispose() }
    return [BitConverter]::ToString($bytes).Replace('-', '').ToLowerInvariant()
}
# Connection uses existing libpq credentials first; prompt locally if unavailable.
function Invoke-Sql([string]$Sql, [string]$Database = 'postgres') {
    $result = $Sql | & $Psql -X -w -q -A -t -v ON_ERROR_STOP=1 -h 127.0.0.1 -p 5432 -U $AdminUser -d $Database 2>&1
    if ($LASTEXITCODE -ne 0) { throw 'Database command failed. Check local PostgreSQL access; credentials are not printed.' }
    return ($result -join "`n").Trim()
}
$taskPreviousPassword = $env:PGPASSWORD
try {
    try { $null = Invoke-Sql 'SELECT 1;' } catch {
        $taskSecret = Read-Host 'Local PostgreSQL administrator password (not saved)' -AsSecureString
        $taskPointer = [Runtime.InteropServices.Marshal]::SecureStringToBSTR($taskSecret)
        try { $env:PGPASSWORD = [Runtime.InteropServices.Marshal]::PtrToStringBSTR($taskPointer) }
        finally { [Runtime.InteropServices.Marshal]::ZeroFreeBSTR($taskPointer) }
        $null = Invoke-Sql 'SELECT 1;'
    }
    if ((Invoke-Sql "SELECT current_setting('server_version_num')::int / 10000;") -ne '18') { throw 'PostgreSQL 18 required.' }
    # Preflight ALL targets before any change. Never adopt/reset unknown existing databases or roles.
    foreach ($kind in @('dev','test')) {
        $db = "shuntian_blog_$kind"
        $file = Join-Path $taskLocal "$kind.env"
        $exists = Invoke-Sql "SELECT count(*) FROM pg_database WHERE datname='$db';"
        $roles = Invoke-Sql "SELECT count(*) FROM pg_roles WHERE rolname IN ('$($db)_owner','$($db)_app');"
        if (($exists -ne '0' -or $roles -ne '0') -and -not (Test-Path $file)) { throw "Existing $db or roles found without local configuration. Refusing to overwrite; inspect manually." }
        if ((Test-Path $file) -and ($exists -ne '1' -or $roles -ne '2')) { throw "Incomplete previous setup for $db; inspect manually." }
    }
    foreach ($kind in @('dev','test')) {
        $db = "shuntian_blog_$kind"
        $file = Join-Path $taskLocal "$kind.env"
        if (Test-Path $file) { Write-Output "$db already configured; unchanged."; continue }
        $ownerPassword = New-Secret
        $appPassword = New-Secret
        $buildToken = New-Secret
        $null = Invoke-Sql "CREATE ROLE $($db)_owner LOGIN PASSWORD '$ownerPassword' NOSUPERUSER NOCREATEDB NOCREATEROLE; CREATE ROLE $($db)_app LOGIN PASSWORD '$appPassword' NOSUPERUSER NOCREATEDB NOCREATEROLE;"
        $null = Invoke-Sql "CREATE DATABASE $db OWNER $($db)_owner ENCODING 'UTF8' TEMPLATE template0;"
        $null = Invoke-Sql "REVOKE ALL ON DATABASE $db FROM PUBLIC; GRANT CONNECT ON DATABASE $db TO $($db)_owner,$($db)_app;"
        $null = Invoke-Sql "REVOKE ALL ON SCHEMA public FROM PUBLIC; GRANT USAGE,CREATE ON SCHEMA public TO $($db)_owner; GRANT USAGE ON SCHEMA public TO $($db)_app;" $db
        @"
DATABASE_URL=postgres://$($db)_app:$appPassword@127.0.0.1:5432/$($db)?sslmode=disable
MIGRATION_DATABASE_URL=postgres://$($db)_owner:$ownerPassword@127.0.0.1:5432/$($db)?sslmode=disable
APP_ROLE=$($db)_app
LISTEN_ADDR=127.0.0.1:8080
PUBLIC_ORIGIN=http://127.0.0.1:8081
COOKIE_SECURE=false
BUILD_TOKEN=$buildToken
"@ | Set-Content -LiteralPath $file -Encoding utf8
        Write-Output "$db created; local configuration saved (credentials hidden)."
    }
} finally { $env:PGPASSWORD = $taskPreviousPassword }
