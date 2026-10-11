@echo off
setlocal
cd /d "%~dp0"
go build -trimpath -ldflags "-s -w" -o dow-mod-studio.exe .
endlocal
