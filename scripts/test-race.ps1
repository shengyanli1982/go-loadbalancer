param(
    [Parameter(ValueFromRemainingArguments = $true)]
    [string[]]$GoTestArgs = @("./...")
)

$ErrorActionPreference = "Stop"

$root = Split-Path -Parent $PSScriptRoot
$base = Join-Path $env:SystemDrive "go-race-cache"
$tmp = Join-Path $base "tmp"
$cache = Join-Path $base "cache"
$mod = Join-Path $base "mod"

New-Item -ItemType Directory -Force -Path $tmp, $cache, $mod | Out-Null

$env:TEMP = $tmp
$env:TMP = $tmp
$env:TMPDIR = $tmp
$env:GOCACHE = $cache
$env:GOMODCACHE = $mod

Write-Host "[race-env] TEMP=$($env:TEMP)"
Write-Host "[race-env] TMP=$($env:TMP)"
Write-Host "[race-env] GOCACHE=$($env:GOCACHE)"
Write-Host "[race-env] GOMODCACHE=$($env:GOMODCACHE)"

Push-Location $root
try {
    & go test -race @GoTestArgs
    exit $LASTEXITCODE
} finally {
    Pop-Location
}
