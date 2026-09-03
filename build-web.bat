@echo off
REM [Trae] Build LitePan web frontend and repack assets into internal/api/web.
REM Purpose: regenerate the frontend bundle so the "Drama Transfer Task" menu appears.
REM Then restart the server: go run ./cmd/litepan
REM Note: all text here is pure ASCII (no Chinese) to avoid cmd/GBK encoding issues.
setlocal enabledelayedexpansion
cd /d "%~dp0web"

echo [1/4] Cleaning old node_modules ...
if exist node_modules rmdir /s /q node_modules

echo [2/4] Installing frontend dependencies (npm install) ...
call npm install --no-audit --no-fund --loglevel=error
if errorlevel 1 goto install_retry

:build
echo [3/4] Building frontend (vue-tsc + vite build -> internal/api/web) ...
call npm run build
if errorlevel 1 goto fail

echo [4/4] Frontend build succeeded!
echo.
echo Please stop the running go run, then re-run in the LitePan folder:
echo     go run ./cmd/litepan
echo.
pause
exit /b 0

:install_retry
echo npm install failed. If it reports EAI_FAIL/ECONNREFUSED, the corporate network
echo is blocking node's DNS lookup. Starting a local DNS proxy and retrying ...
start "npm-proxy" cmd /k node npm-proxy.cjs
timeout /t 3 /nobreak >nul
call npm install --no-audit --no-fund --loglevel=error --proxy=http://127.0.0.1:8765 --https-proxy=http://127.0.0.1:8765
if errorlevel 1 goto fail
goto build

:fail
echo Build failed. Please check the error messages above.
pause
exit /b 1
