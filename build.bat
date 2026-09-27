@echo off
setlocal enabledelayedexpansion

REM ---------------------------------------------------------------------
REM build.bat
REM
REM Produces a distributable Windows build of AlwaysOnTop:
REM   1. Packages the Python engine into a single, dependency-free .exe
REM      using PyInstaller (so end users do not need Python installed),
REM      writing it to backend\embedded\AlwaysOnTopEngine.exe.
REM   2. Builds the Wails/Go application with `-tags production`, which
REM      embeds that engine .exe directly inside AlwaysOnTop.exe (see
REM      pyengine_embed_prod.go). At runtime the app extracts it to
REM      %AppData%\AlwaysOnTop\backend the first time it's needed, so
REM      every file the app writes lives in a normal per-user location -
REM      never inside the (possibly read-only) install folder.
REM   3. If NSIS's makensis is on PATH, also builds a proper Windows
REM      installer (Start Menu entry, uninstaller) via `wails build -nsis`.
REM      If it isn't installed, this step is skipped with a warning and
REM      you still get a fully self-contained AlwaysOnTop.exe.
REM
REM IMPORTANT: run this from a Command Prompt window you opened yourself
REM (Win+R -> cmd -> cd to the project folder -> build.bat), not by
REM double-clicking it in Explorer - see the pause at the end either way.
REM ---------------------------------------------------------------------

set ROOT=%~dp0
set BACKEND_DIR=%ROOT%backend
set DIST_DIR=%ROOT%build\bin

echo ROOT dir:    %ROOT%
echo Backend dir: %BACKEND_DIR%
echo.

echo.
echo === [1/3] Building Python engine with PyInstaller ===
echo.

if not exist "%BACKEND_DIR%\engine.py" (
    echo ERROR: could not find "%BACKEND_DIR%\engine.py".
    echo Make sure you are running build.bat from the AlwaysOnTop project root
    echo ^(the folder that directly contains go.mod, main.go, and backend\^).
    goto :fail
)

pushd "%BACKEND_DIR%"

where python >nul 2>nul
if errorlevel 1 (
    echo ERROR: "python" was not found on PATH.
    echo Install Python 3.10+ from https://www.python.org/downloads/
    echo and make sure "Add python.exe to PATH" is checked during install,
    echo then open a NEW Command Prompt and try again.
    popd
    goto :fail
)

echo Using python:
where python
python --version
echo.

echo Installing/upgrading pip...
python -m pip install --upgrade pip
if errorlevel 1 (
    echo ERROR: failed to upgrade pip. See the output above for details.
    popd
    goto :fail
)

echo.
echo Installing backend requirements...
python -m pip install -r requirements.txt
if errorlevel 1 (
    echo ERROR: failed to install backend\requirements.txt.
    echo This usually means pywin32 failed to install - check the pip
    echo output above for the actual error.
    popd
    goto :fail
)

echo.
echo Installing PyInstaller...
python -m pip install pyinstaller
if errorlevel 1 (
    echo ERROR: failed to install pyinstaller.
    popd
    goto :fail
)

echo.
echo Running PyInstaller...
REM Invoked as "python -m PyInstaller" rather than the bare "pyinstaller"
REM command: pip installs the pyinstaller.exe script into Python's
REM Scripts\ folder, which is frequently NOT on PATH even when "python"
REM itself is - going through "python -m" sidesteps that entirely.
REM
REM --distpath embedded: this is the exact path Go's
REM   //go:embed backend/embedded/AlwaysOnTopEngine.exe
REM directive in pyengine_embed_prod.go reads from - do not rename
REM without updating that file too.
REM --onefile: single portable exe.
REM --noconsole: no console window flashes up when the parent spawns it
REM              (also enforced on the Go side via CREATE_NO_WINDOW).
REM --name: matches exeSuffix("AlwaysOnTopEngine") used throughout Go code.
python -m PyInstaller --onefile --noconsole --name AlwaysOnTopEngine --distpath embedded --workpath build --specpath build engine.py

if errorlevel 1 (
    echo ERROR: PyInstaller build failed. See the output above for details.
    popd
    goto :fail
)

if not exist "embedded\AlwaysOnTopEngine.exe" (
    echo ERROR: PyInstaller reported success but embedded\AlwaysOnTopEngine.exe
    echo is missing. Check the PyInstaller output above.
    popd
    goto :fail
)

popd

echo.
echo === [2/3] Building Wails application ===
echo.

where wails >nul 2>nul
if errorlevel 1 (
    echo ERROR: "wails" CLI was not found on PATH.
    echo Install it with:
    echo   go install github.com/wailsapp/wails/v2/cmd/wails@latest
    echo.
    echo Then make sure your Go bin folder is on PATH ^(usually
    echo %%USERPROFILE%%\go\bin^), open a NEW Command Prompt, and try again.
    echo You can check with: where wails
    goto :fail
)

echo Using wails:
where wails
call wails version
echo.

where makensis >nul 2>nul
if errorlevel 1 (
    set NSIS_FLAG=
    echo NOTE: NSIS ^(makensis^) was not found on PATH - building a plain
    echo AlwaysOnTop.exe without a Windows installer this time. It is
    echo still fully self-contained ^(the Python engine is embedded^), so
    echo you can distribute that single file as-is if you don't need an
    echo installer. To also get a proper installer with a Start Menu
    echo entry and uninstaller, install NSIS ^(e.g. "winget install
    echo NSIS.NSIS"^), make sure its Bin folder ^(containing makensis.exe^)
    echo is on PATH, and re-run build.bat.
) else (
    set NSIS_FLAG=-nsis
    echo Using makensis:
    where makensis
)
echo.

REM -tags production selects pyengine_embed_prod.go over
REM pyengine_embed_dev.go, embedding the just-built engine .exe directly
REM into AlwaysOnTop.exe. Without this tag you'd get a dev-mode binary
REM that expects a local Python interpreter on PATH at runtime.
call wails build -clean -tags production %NSIS_FLAG%
if errorlevel 1 (
    echo ERROR: "wails build" failed. See the output above for the actual
    echo compiler/toolchain error - common causes are a missing Go
    echo installation, a stale frontend\wailsjs folder, or ^(if using
    echo -nsis^) an NSIS scripting error. Try running "wails doctor" for
    echo general diagnostics.
    goto :fail
)

if not exist "%DIST_DIR%\AlwaysOnTop.exe" (
    echo ERROR: expected "%DIST_DIR%\AlwaysOnTop.exe" was not produced.
    echo "wails build" reported success but the binary is missing - check
    echo the wails build output above.
    goto :fail
)

echo.
echo === [3/3] Done ===
echo.
echo Distributable output: %DIST_DIR%
echo   AlwaysOnTop.exe                       ^(self-contained - engine is embedded^)
if defined NSIS_FLAG (
    echo   AlwaysOnTop-amd64-installer.exe       ^(Windows installer^)
    echo.
    echo Hand people the installer .exe - it installs the app, adds a Start
    echo Menu entry, and registers an uninstaller. AlwaysOnTop.exe itself
    echo can also be run standalone from anywhere without installing.
) else (
    echo.
    echo No installer was built ^(NSIS not found - see the note above^).
    echo AlwaysOnTop.exe alone is a complete, standalone application; copy
    echo just that one file to distribute it.
)
echo.
echo On first run, the app extracts its embedded Python engine into:
echo   %%AppData%%\AlwaysOnTop\backend\AlwaysOnTopEngine.exe
echo No files are required next to AlwaysOnTop.exe itself.
echo.
goto :end

:fail
echo.
echo === Build FAILED - see the error above ===
echo.
endlocal
pause
exit /b 1

:end
endlocal
pause
exit /b 0
