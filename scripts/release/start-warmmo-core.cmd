@echo off
setlocal

set "WARMMO_SKILLS_DIR=%~dp0skills"
"%~dp0bin\warmmo-core.exe"

if errorlevel 1 pause
