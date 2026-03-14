@echo off
setlocal
set BASE=%SystemDrive%\go-race-cache
set TEMP=%BASE%\tmp
set TMP=%BASE%\tmp
set TMPDIR=%BASE%\tmp
set GOCACHE=%BASE%\cache
set GOMODCACHE=%BASE%\mod
if not exist "%TEMP%" mkdir "%TEMP%"
if not exist "%GOCACHE%" mkdir "%GOCACHE%"
if not exist "%GOMODCACHE%" mkdir "%GOMODCACHE%"

echo [race-env] TEMP=%TEMP%
echo [race-env] TMP=%TMP%
echo [race-env] GOCACHE=%GOCACHE%
echo [race-env] GOMODCACHE=%GOMODCACHE%

go test -race %*
exit /b %errorlevel%
