@echo off
REM Double-clickable runtime launcher for Windows.
REM Uses .env for the backend/model/key so no flags are needed.
setlocal
cd /d "%~dp0"
where py >nul 2>nul
if %ERRORLEVEL%==0 (
    py -3 -m runtime.main %*
) else (
    python -m runtime.main %*
)
echo.
echo Runtime stopped.
pause
endlocal
