@echo off
rem Builds the Windows version resource the executable carries: without it the
rem binary is a file with no description, no version and no publisher, which is
rem exactly the shape antivirus heuristics score as suspicious.
rem
rem The resource is compiled with mingw's windres, which this machine has inside
rem WSL; the result is named for windows/amd64 so Linux builds ignore it.
setlocal
cd /d "%~dp0"
set "SRC=%~dp0"
set "SRC=%SRC:~0,-1%"

wsl --version >nul 2>&1
if errorlevel 1 (
    echo Cannot generate the Windows resource: WSL is not available.
    exit /b 1
)

wsl bash -lc "command -v x86_64-w64-mingw32-windres >/dev/null" 2>nul
if errorlevel 1 (
    echo Cannot generate the Windows resource: x86_64-w64-mingw32-windres is not installed in WSL.
    exit /b 1
)

echo Generating resource_windows_amd64.syso...
wsl bash -lc "cd \"$(wslpath -a '%SRC%')\" && x86_64-w64-mingw32-windres -o resource_windows_amd64.syso versioninfo.rc"
if errorlevel 1 (
    echo Failed to run windres on versioninfo.rc.
    exit /b 1
)

if not exist "resource_windows_amd64.syso" (
    echo windres reported success but resource_windows_amd64.syso is missing.
    exit /b 1
)
echo Generated resource_windows_amd64.syso.
