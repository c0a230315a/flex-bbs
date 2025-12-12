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

set "JAVA_BIN=%BASE_DIR%..\flexible-ipfs-runtime\win-x64\jre\bin\java.exe"
if exist "%JAVA_BIN%" (
  "%JAVA_BIN%" -cp "lib/*" org.peergos.APIServer
) else (
  java -cp "lib/*" org.peergos.APIServer
)

pause
endlocal
