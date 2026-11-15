@echo off
rem Switch the console to UTF-8 to display Unicode output correctly.
chcp 65001 >nul 2>&1
rem ARTEX daemon start script (Windows)
rem
rem Usage:
rem   start.bat                  Run in the foreground (Ctrl-C to stop)
rem   start.bat -addr :9000      Pass additional arguments directly to artex
rem
rem This script starts artex.exe and uses its exit code to decide whether to restart it.
rem
rem   0      Normal user shutdown -> exit the loop
rem   75     Restart requested -> restart immediately (one-click update or rollback in the UI)
rem   Other  Abnormal exit -> restart with backoff (1, 2, 4, ... up to 60 seconds)
rem
rem Downloads, SHA256 verification, and version replacement are handled by artex at startup
rem (the selfupdate package). Keep this script simple; see the introduction in start.sh for details.

setlocal enabledelayedexpansion
cd /d "%~dp0"

set "BIN=artex.exe"
if not exist "%BIN%" (
	echo [artex] Executable not found: %BIN% 1>&2
	exit /b 1
)

set "RESTART_CODE=75"
set "MAX_DELAY=60"
set /a delay=1

:loop
"%BIN%" %*
set "code=!ERRORLEVEL!"

if "!code!"=="0" (
	echo [artex] Exited normally
	exit /b 0
)

if "!code!"=="%RESTART_CODE%" (
	rem Update/rollback is staged. Restarting completes version replacement at startup.
	echo [artex] Restart requested: applying the new version...
	set /a delay=1
	goto loop
)

echo [artex] Abnormal exit ^(code=!code!^), restarting in !delay!s 1>&2
rem timeout fails with a redirected console; use ping instead (N seconds requires N+1 attempts).
set /a pings=!delay!+1
ping -n !pings! 127.0.0.1 >nul 2>&1
set /a delay=!delay!*2
if !delay! gtr %MAX_DELAY% set /a delay=%MAX_DELAY%
goto loop
