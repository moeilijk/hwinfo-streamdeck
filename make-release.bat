@echo off
setlocal
pushd "%~dp0" || exit /b 1

if not exist "build" mkdir "build"
del /q "build\com.moeilijk.hwinfo.streamDeckPlugin" 2>nul

call streamdeck validate "com.moeilijk.hwinfo.sdPlugin"
if errorlevel 1 exit /b 1
call streamdeck pack "com.moeilijk.hwinfo.sdPlugin" --output "build" --force

popd
endlocal
