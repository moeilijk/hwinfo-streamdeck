@echo off
setlocal
pushd "%~dp0" || exit /b 1

if not exist "build" mkdir "build"
del /q "build\com.exension.hwinfo.streamDeckPlugin" 2>nul

call streamdeck validate "com.exension.hwinfo.sdPlugin"
if errorlevel 1 exit /b 1
call streamdeck pack "com.exension.hwinfo.sdPlugin" --output "build" --force

popd
endlocal
