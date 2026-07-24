@echo off
setlocal
pushd "%~dp0" || exit /b 1

call ".\kill-streamdeck.bat"

xcopy "com.exension.hwinfo.sdPlugin" "%APPDATA%\Elgato\StreamDeck\Plugins\com.exension.hwinfo.sdPlugin\" /E /I /Q /Y

call ".\start-streamdeck.bat"

popd
endlocal
