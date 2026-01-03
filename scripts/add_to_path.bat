@echo off
setlocal EnableExtensions EnableDelayedExpansion

REM Get directory of this .bat file
set "SCRIPT_DIR=%~dp0"
set "SCRIPT_DIR=%SCRIPT_DIR:~0,-1%"

REM Read User PATH
for /f "tokens=2,*" %%A in (
    'reg query HKCU\Environment /v Path 2^>nul'
) do set "USERPATH=%%B"

if not defined USERPATH set "USERPATH="

REM Avoid duplicates
echo !USERPATH! | find /I "%SCRIPT_DIR%" >nul
if not errorlevel 1 (
    echo Already in PATH:
    echo %SCRIPT_DIR%
    goto :notify
)

REM Write PATH
reg add HKCU\Environment /v Path /t REG_EXPAND_SZ /d "!USERPATH!;%SCRIPT_DIR%" /f >nul

echo Added to PATH:
echo %SCRIPT_DIR%

:notify
REM Notify running applications (same as pressing Save)
powershell -NoProfile -Command ^
  "[Environment]::SetEnvironmentVariable('Path', [Environment]::GetEnvironmentVariable('Path','User'),'User')"

echo.
echo PATH updated. New terminals will see it immediately.
pause
