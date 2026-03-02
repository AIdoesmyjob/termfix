@echo off
setlocal enabledelayedexpansion

set "SCRIPT_DIR=%~dp0"
set "LLAMA_SERVER=%SCRIPT_DIR%bin\llama-server.exe"
set "MODEL_DIR=%SCRIPT_DIR%models"
set "SERVER_LOG="

:: Validate TERMFIX_MODEL if set
if defined TERMFIX_MODEL (
    if not exist "%TERMFIX_MODEL%" (
        echo ERROR: TERMFIX_MODEL file not found: %TERMFIX_MODEL%
        exit /b 1
    )
    set "MODEL=%TERMFIX_MODEL%"
    goto :model_found
)

:: Find model (prefer qwen15b)
set "MODEL="
for %%f in ("%MODEL_DIR%\*qwen15b*q4_k_m*") do set "MODEL=%%f"
if "!MODEL!"=="" for %%f in ("%MODEL_DIR%\*.gguf") do set "MODEL=%%f"
if "!MODEL!"=="" (
    echo ERROR: No model found in %MODEL_DIR%
    echo.
    echo Download a model from the GitHub release and place it in:
    echo   %MODEL_DIR%\
    exit /b 1
)
:model_found

:: Validate port
if not defined TERMFIX_PORT set "TERMFIX_PORT=8012"
set "PORT_VALID=1"
for /f "delims=0123456789" %%i in ("%TERMFIX_PORT%") do set "PORT_VALID=0"
if "%TERMFIX_PORT%"=="" set "PORT_VALID=0"
if "%PORT_VALID%"=="0" (
    echo ERROR: Invalid port: %TERMFIX_PORT% ^(must be 1-65535^)
    exit /b 1
)
if %TERMFIX_PORT% LSS 1 (
    echo ERROR: Invalid port: %TERMFIX_PORT% ^(must be 1-65535^)
    exit /b 1
)
if %TERMFIX_PORT% GTR 65535 (
    echo ERROR: Invalid port: %TERMFIX_PORT% ^(must be 1-65535^)
    exit /b 1
)
set "LOCAL_ENDPOINT=http://127.0.0.1:%TERMFIX_PORT%"
set "SERVER_LOG=%SCRIPT_DIR%.llama-server-%TERMFIX_PORT%.log"

for %%F in ("%MODEL%") do set "MODEL_BASE=%%~nxF"
echo Starting llama-server on port %TERMFIX_PORT% with %MODEL_BASE%...

:: Start server with output redirected to log file
start /B "" cmd /c ""%LLAMA_SERVER%" --model "%MODEL%" -c 8192 --flash-attn on --cache-type-k q8_0 --cache-type-v q8_0 --host 127.0.0.1 --port %TERMFIX_PORT% --parallel 1 >"%SERVER_LOG%" 2>&1"

:: Capture the PID of the server
set "SERVER_PID="
ping -n 2 127.0.0.1 >nul 2>&1
for /f "tokens=2" %%p in ('tasklist /fi "imagename eq llama-server.exe" /fo list 2^>nul ^| findstr /i "PID:"') do (
    set "SERVER_PID=%%p"
)

:: Wait for server to be ready (up to 60 attempts)
set /a "WAIT_COUNT=0"
set /a "WAIT_MAX=60"
:wait_loop
set /a "WAIT_COUNT+=1"
if %WAIT_COUNT% GTR %WAIT_MAX% (
    echo.
    echo ERROR: llama-server failed to become ready within %WAIT_MAX% seconds.
    if exist "%SERVER_LOG%" (
        echo Log output:
        powershell -Command "Get-Content '%SERVER_LOG%' | Select-Object -Last 20"
    )
    goto :cleanup_exit
)

:: Check if server process is still alive
if defined SERVER_PID (
    tasklist /fi "pid eq %SERVER_PID%" /fo csv /nh 2>nul | findstr /i "llama-server" >nul 2>&1
    if errorlevel 1 (
        echo.
        echo ERROR: llama-server failed to start.
        if exist "%SERVER_LOG%" (
            echo Log output:
            powershell -Command "Get-Content '%SERVER_LOG%' | Select-Object -Last 20"
            findstr /c:"couldn't bind" "%SERVER_LOG%" >nul 2>&1
            if not errorlevel 1 (
                echo.
                echo Port %TERMFIX_PORT% is already in use. Try: set TERMFIX_PORT=8013 ^&^& %~nx0
            )
        )
        goto :cleanup_exit
    )
)

curl -sf "%LOCAL_ENDPOINT%/health" 2>nul | findstr /c:"\"ok\"" >nul 2>&1
if errorlevel 1 (
    <nul set /p "=."
    ping -n 2 127.0.0.1 >nul 2>&1
    goto :wait_loop
)

echo  ready!

:: Auto-generate config after server is confirmed ready
for %%F in ("%MODEL%") do set "MODEL_NAME=%%~nxF"
set "REGEN_CONFIG=0"
if not exist "%SCRIPT_DIR%.termfix.json" (
    set "REGEN_CONFIG=1"
) else (
    findstr /c:"%MODEL_NAME%" "%SCRIPT_DIR%.termfix.json" >nul 2>&1
    if errorlevel 1 set "REGEN_CONFIG=1"
)
if "!REGEN_CONFIG!"=="1" (
    (
        echo {
        echo   "providers": {
        echo     "local": { "apiKey": "dummy" }
        echo   },
        echo   "agents": {
        echo     "coder":      { "model": "local.!MODEL_NAME!" },
        echo     "summarizer": { "model": "local.!MODEL_NAME!" },
        echo     "task":       { "model": "local.!MODEL_NAME!" },
        echo     "title":      { "model": "local.!MODEL_NAME!", "maxTokens": 80 }
        echo   }
        echo }
    ) > "%SCRIPT_DIR%.termfix.json"
)

cd /d "%SCRIPT_DIR%"
"%SCRIPT_DIR%bin\termfix.exe" %*

:cleanup_exit
:: Kill only our instance by PID if we captured it
if defined SERVER_PID (
    taskkill /f /pid %SERVER_PID% >nul 2>&1
)
exit /b %ERRORLEVEL%
