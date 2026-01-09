@echo off
setlocal

rem Base directory = folder containing this script
set "BASE_DIR=%~dp0"
cd /d "%BASE_DIR%"

rem Ensure required local paths exist for first-run
if not exist "providers" mkdir "providers"
if not exist "getdata" mkdir "getdata"
if not exist "attr" type nul > "attr"

set "HOME=%BASE_DIR%"
set "IPFS_HOME=%BASE_DIR%\.ipfs"

rem Optional override for ipfs.endpoint in kadrtt.properties (format: /ip4/<ip>/tcp/4001/ipfs/<PeerID>)
if not "%FLEXIPFS_GW_ENDPOINT%"=="" (
  powershell -NoProfile -ExecutionPolicy Bypass -Command ^
    "$p = Join-Path $env:BASE_DIR 'kadrtt.properties';" ^
    "$e = $env:FLEXIPFS_GW_ENDPOINT;" ^
    "if ($e -match \"[``r``n]\") { throw 'FLEXIPFS_GW_ENDPOINT must be a single line' }" ^
    "$utf8 = New-Object System.Text.UTF8Encoding($false);" ^
    "$c = [IO.File]::ReadAllText($p, $utf8);" ^
    "if ($c -match '(?m)^\\s*ipfs\\.endpoint\\s*[:=].*$') {" ^
    "  $c = [regex]::Replace($c, '(?m)^(\\s*ipfs\\.endpoint\\s*[:=]).*(\\r?)$', '${1}' + $e + '${2}');" ^
    "} else {" ^
    "  if (-not ($c.EndsWith(\"`r`n\") -or $c.EndsWith(\"`n\"))) { $c += \"`r`n\" }" ^
    "  $c += \"ipfs.endpoint=$e`r`n\";" ^
    "}" ^
    "[IO.File]::WriteAllText($p, $c, $utf8);"
)

set "JAVA_BIN=%BASE_DIR%..\flexible-ipfs-runtime\win-x64\jre\bin\java.exe"
if exist "%JAVA_BIN%" (
  "%JAVA_BIN%" -cp "lib/*" org.peergos.APIServer
) else (
  java -cp "lib/*" org.peergos.APIServer
)

pause
endlocal
