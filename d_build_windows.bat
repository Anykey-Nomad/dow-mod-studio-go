@echo off
setlocal
cd /d "%~dp0"
go build -trimpath -ldflags "-s -w -H windowsgui" -o dow-mod-studio.exe .
go build -trimpath -ldflags "-s -w" -o dow-mod-studio-console.exe .
endlocal
