@echo off
REM Double-clickable wrapper around install.ps1.
REM Any arguments are forwarded, e.g.  install.cmd -Check
setlocal
powershell -NoProfile -ExecutionPolicy Bypass -File "%~dp0install.ps1" %*
set EXITCODE=%ERRORLEVEL%
if not "%EXITCODE%"=="0" (
    echo.
    echo Setup exited with code %EXITCODE%.
    pause
)
endlocal & exit /b %EXITCODE%
