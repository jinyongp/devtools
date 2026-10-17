@echo off
setlocal DisableDelayedExpansion
python "%~dp0project.py" %*
exit /b %errorlevel%
