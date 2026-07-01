@echo off
REM ==========================================================
REM up.bat - push to kehuai + GitHub in parallel
REM Run this from the repo root (double-click or: up.bat)
REM ==========================================================

REM Keep output ASCII-clean so GBK/ANSI cmd.exe never splits
REM a URL into garbage commands. No non-ASCII characters below.

echo ===============================================
echo   1/3  Configure remotes (kehuai + origin)
echo ===============================================

git remote get-url kehuai >nul 2>&1
if errorlevel 1 (
    git remote add kehuai https://git.kehuai.fun:10086/misu/wx_web_api.git
) else (
    git remote set-url kehuai https://git.kehuai.fun:10086/misu/wx_web_api.git
)

git remote get-url origin >nul 2>&1
if errorlevel 1 (
    git remote add origin https://github.com/kehuai007/wx_web_api.git
) else (
    git remote set-url origin https://github.com/kehuai007/wx_web_api.git
)

echo.
echo Current remotes:
git remote -v
echo.

echo ===============================================
echo   2/3  Ensure current branch is named main
echo ===============================================
git branch -M main
echo.

echo ===============================================
echo   3/3  Push to both remotes in parallel
echo ===============================================
echo Launching two background windows...
echo.

REM Each 'start' returns immediately, so the two pushes run
REM concurrently. /k keeps the window open; pause lets you
REM read the result before closing it.
start "PUSH-kehuai" cmd /k "git push -u kehuai main ^&^& echo. ^&^& echo === DONE: kehuai push === ^&^& pause"
start "PUSH-origin" cmd /k "git push -u origin  main ^&^& echo. ^&^& echo === DONE: origin push === ^&^& pause"

echo Both pushes started. Wait for both windows to finish.
echo ===============================================
