@echo off
setlocal
cd /d "%~dp0"
rem The Windows resource gives the exe its name, version and publisher; a bare
rem unsigned binary is what antivirus heuristics flag, so it is generated first
rem and a failure only warns: the build itself must still work without WSL.
call "%~dp0gen_windows_res.bat"
if errorlevel 1 echo warning: the Windows version resource was not generated
go build -trimpath -ldflags "-s -w -H windowsgui" -o dow-mod-studio.exe .
go build -trimpath -ldflags "-s -w" -o dow-mod-studio-console.exe .
set GOOS=linux& set GOARCH=amd64& set CGO_ENABLED=0& go build -o dow-mod-studio .

endlocal
