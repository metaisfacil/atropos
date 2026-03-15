@echo off
REM Atropos Wails Setup Script for Windows

echo.
echo ====================================
echo  Atropos Wails Setup
echo ====================================
echo.

REM Check if Go is installed
where /q go
if errorlevel 1 (
    echo ERROR: Go is not installed or not in PATH
    echo Please install Go 1.21+ from https://golang.org
    pause
    exit /b 1
)
echo [OK] Go is installed

REM Check if Node.js is installed
where /q npm
if errorlevel 1 (
    echo ERROR: Node.js/npm is not installed or not in PATH
    echo Please install Node.js from https://nodejs.org
    pause
    exit /b 1
)
echo [OK] Node.js is installed

REM Install Wails CLI
echo.
echo Installing Wails CLI...
go install github.com/wailsapp/wails/v2/cmd/wails@latest
if errorlevel 1 (
    echo ERROR: Failed to install Wails CLI
    pause
    exit /b 1
)
echo [OK] Wails CLI installed

REM Install frontend dependencies
echo.
echo Installing frontend dependencies...
cd frontend
if not exist node_modules (
    npm install
    if errorlevel 1 (
        echo ERROR: Failed to install npm dependencies
        pause
        exit /b 1
    )
)
echo [OK] Frontend dependencies installed
cd ..

REM Download go.sum entries
echo.
echo Downloading Go dependencies...
go mod download
go mod tidy
if errorlevel 1 (
    echo WARNING: Failed to sync Go dependencies
    echo This might be normal if you're offline
)
echo [OK] Go dependencies synced

echo.
echo ====================================
echo  Setup Complete!
echo ====================================
echo.
echo Next steps:
echo   - For development: wails dev
echo   - To build:        wails build -o atropos.exe
echo   - Or use:          make dev
echo                      make build
echo.
pause
    echo    .\vcpkg integrate install
    echo    .\vcpkg install opencv:x64-windows
    echo.
    echo 2. Using Chocolatey:
    echo    choco install opencv
    echo.
    echo 3. Manual: Download from https://opencv.org/releases/
    echo.
    echo After installation, set OPENCV_DIR to your OpenCV directory
    echo.
) else (
    echo [OK] OPENCV_DIR is set to: %OPENCV_DIR%
)

REM Install Go dependencies
echo.
echo Installing Go dependencies...
echo.

go get -u github.com/webview/webview_go
if errorlevel 1 (
    echo [!] Failed to get webview_go
    goto :error
)

go get -u -d gocv.io/x/gocv
if errorlevel 1 (
    echo [!] Failed to get gocv
    echo This is expected if OpenCV is not yet installed
) else (
    echo [OK] gocv installed
)

echo.
echo ====================================
echo Setup Complete!
echo ====================================
echo.
echo To build Atropos:
echo   go build -o atropos.exe main.go
echo.
echo To run directly:
echo   go run main.go
echo.
echo For more information, see README.md
echo.

exit /b 0

:error
echo.
echo Setup encountered errors. Please check the output above.
exit /b 1
