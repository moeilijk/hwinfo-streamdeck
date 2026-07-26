@echo off
setlocal
pushd "%~dp0" || exit /b 1

call ".\kill-streamdeck.bat"

xcopy "com.moeilijk.hwinfo.sdPlugin" "%APPDATA%\Elgato\StreamDeck\Plugins\com.moeilijk.hwinfo.sdPlugin\" /E /I /Q /Y

call ".\start-streamdeck.bat"

popd
endlocal
