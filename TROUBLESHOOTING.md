# Wails Atropos - Troubleshooting Guide

## Common Issues and Solutions

### 1. "command not found: wails"

**Symptom:** Running `wails dev` gives "command not found"

**Solution:**
```bash
# Install Wails globally
go install github.com/wailsapp/wails/v2/cmd/wails@latest

# Add Go bin to PATH (if needed)
# Windows PowerShell:
$env:PATH = "$env:PATH;$(go env GOPATH)\bin"

# Windows CMD:
set PATH=%PATH%;%GOPATH%\bin

# Verify
wails --version
```

---

### 2. npm modules not found

**Symptom:** Error like "module not found" when running `wails dev`

**Solution:**
```bash
cd frontend
npm install
npm list  # Check all modules installed
cd ..
wails dev
```

---

### 3. gocv build errors

**Symptom:** `"opencv2/opencv.hpp" not found` or similar

**Solution - Already have OpenCV?**
```bash
# Set environment variable
set OPENCV_DIR=C:\tools\opencv\build

# If that doesn't work, try:
set OPENCV_DIR=C:\path\to\opencv

# Then try building
wails build
```

**Solution - Need to install OpenCV:**

Windows with vcpkg:
```bash
# Install vcpkg
git clone https://github.com/Microsoft/vcpkg.git
cd vcpkg
.\bootstrap-vcpkg.bat

# Install OpenCV
.\vcpkg install opencv:x64-windows

# Set environment variable
set OPENCV_DIR=C:\path\to\vcpkg\installed\x64-windows
```

**Or use pre-built OpenCV:**
1. Download from https://github.com/opencv/opencv/releases
2. Extract to `C:\tools\opencv`
3. Set `OPENCV_DIR=C:\tools\opencv\build`

---

### 4. Frontend not showing in dev mode

**Symptom:** App window opens but shows blank/white

**Solution:**
```bash
# Stop current dev server (Ctrl+C)

# Rebuild frontend
cd frontend
rm -r dist
npm run build
cd ..

# Start dev mode again
wails dev
```

---

### 5. Changes not hot-reloading

**Symptom:** Edit frontend/src/App.jsx but changes don't appear

**Solution:**
```bash
# Frontend (React) should auto-reload
# If it doesn't:

# 1. Check Vite is running (should see port in terminal)
# 2. Hard refresh browser (Ctrl+Shift+R or Cmd+Shift+R)
# 3. Restart:
#    - Stop wails dev
#    - Delete frontend/dist
#    - Run: wails dev

# Backend (Go) changes require restart
# App will auto-restart when you save .go files
```

---

### 6. "cannot find package" errors

**Symptom:** Go compilation errors like `"cannot find github.com/wailsapp/wails/v2"`

**Solution:**
```bash
# Sync Go modules
go mod download
go mod tidy

# Verify modules are present
go mod verify

# Try building again
wails build
```

---

### 7. Port/connection issues

**Symptom:** "Address already in use" or connection errors

**Solution:**
```bash
# Wails uses random ports, so this is rare
# If it happens, try killing any existing processes:

# Windows PowerShell:
Get-Process wails -ErrorAction SilentlyContinue | Stop-Process -Force
Get-Process node -ErrorAction SilentlyContinue | Stop-Process -Force

# Then restart
wails dev
```

---

### 8. Image loading fails

**Symptom:** Click "Load Image" but nothing happens

**Solution:**
1. Check browser console (F12) for errors
2. Verify file permissions on the image
3. Try a different image format (JPG, PNG)
4. Check file path is valid

**Debug:**
```bash
# Add some logging to see what's happening
# In app.go, add print statements in LoadImage()
# Rebuild and check terminal output
```

---

### 9. Image processing is slow

**Symptom:** Takes >5 seconds to detect corners

**Possible causes:**
- Image too large (>4K resolution)
- Max corners value too high (>500)
- Low quality level value too high (>10)

**Solution:**
```bash
# Try these values first:
# Max Corners: 200-500
# Quality Level: 0.1-1.0
# Min Distance: 50-100
# Accent: 0

# For large images:
# 1. Reduce image size first
# 2. Use lower corner count
# 3. Reduce quality threshold
```

---

### 10. Build creates huge .exe

**Symptom:** Output binary is 300MB+

**Solution:**
```bash
# Use strip flags to reduce size
wails build -ldflags "-s -w"

# Or in Makefile, modify build target:
build:
    wails build -ldflags "-s -w" -o atropos.exe
```

---

### 11. "react/jsx-runtime" not found

**Symptom:** Build error mentioning JSX runtime

**Solution:**
```bash
cd frontend
npm install react react-dom
npm install --save-dev @vitejs/plugin-react
cd ..

# Then rebuild
wails build
```

---

### 12. Changes to wails.json not taking effect

**Symptom:** Change window title in wails.json but it doesn't update

**Solution:**
```bash
# Changes to wails.json require rebuild, not just dev restart
# Stop dev mode
# Rebuild:
wails build

# Or start dev again
wails dev
```

---

## Quick Diagnostics

Run these to help diagnose issues:

```bash
# Check Go version
go version

# Check Node version
npm --version
node --version

# Check Wails
wails --version

# Check if OpenCV is accessible
go list gocv.io/x/gocv

# Check if modules are installed
go mod verify

# List npm packages
npm list --depth=0
```

---

## Still Stuck?

1. **Check the logs:**
   - Terminal output when running `wails dev`
   - Browser console (F12)
   - Check `app.go` for any error messages

2. **Try a clean rebuild:**
   ```bash
   # Full clean
   go clean -cache
   go clean -modcache
   cd frontend && rm -r node_modules dist && npm install && cd ..
   
   # Full rebuild
   wails build
   ```

3. **Check GitHub Issues:**
   - Wails: https://github.com/wailsapp/wails/issues
   - gocv: https://github.com/hybridgroup/gocv/issues
   - React: https://github.com/facebook/react/issues

4. **Ask on forums:**
   - Wails Discussions: https://github.com/wailsapp/wails/discussions
   - Go on StackOverflow (tag: go, gocv)

---

## Performance Optimization Tips

If things are running slow:

1. **Reduce image size** before processing
2. **Lower corner detection count** (start with 100)
3. **Increase min distance** for corners
4. **Use lower quality threshold** for detection
5. **Close other applications** to free RAM

## Memory Usage

Expected memory usage:
- Base app: ~50MB
- With 1920x1440 image: ~200MB
- During processing: up to 300MB

If exceeding 500MB+:
- Close and reopen app
- Reduce image size
- Restart system

---

Last updated: 2024
For latest Wails documentation, visit https://wails.io
