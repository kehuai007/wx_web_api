@echo off
setlocal

echo Killing any process on port 13335...
for /f "tokens=5" %%a in ('netstat -ano ^| findstr :13335 ^| findstr LISTENING') do taskkill /PID %%a /F 2>nul
del dist\app.log
set DIST_DIR=%~dp0
cd /d "%DIST_DIR%"

echo Building wx_web_api...
go build -ldflags "-s -w" -o dist/wx_web_api.exe .

echo Build complete: dist/wx_web_api.exe
