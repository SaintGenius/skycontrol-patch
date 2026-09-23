@echo off
cd /d "%~dp0"
certutil -decode "%~dp0skycontrol-callsign-fix.b64" "%~dp0skycontrol-callsign-fix.zip"
tar -xf "%~dp0skycontrol-callsign-fix.zip"
echo.
echo Replaced internal\atc\center.go and internal\atc\tower.go
echo Now run BUILD.bat
pause
