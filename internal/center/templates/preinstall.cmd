@echo off
where powershell.exe >nul 2>nul
if errorlevel 1 goto blocked
powershell.exe -NoProfile -ExecutionPolicy Bypass -File X:\ventoy-center\guard.ps1
if errorlevel 1 goto blocked
exit /b 0
:blocked
echo Installation guard failed. No further automatic steps are allowed.
echo Check WinPE PowerShell, network, disk identity and controller task status.
ping -n 61 127.0.0.1 >nul
goto blocked
