.PHONY: build dev clean setup frontend test help ensure-prereqs debug

# ---------------------------------------------------------------------------
# Platform detection
# ---------------------------------------------------------------------------
ifeq ($(OS),Windows_NT)
EXE_NAME := atropos.exe
# Path separator and null device differ between Windows and Unix.
PATHSEP := ;
NULL := NUL
# Go's GOPATH uses backslashes on Windows; append \bin for the tools dir.
GOBIN_DIR := $(shell go env GOPATH 2>NUL)\bin
else
EXE_NAME := atropos
PATHSEP := :
NULL := /dev/null
GOBIN_DIR := $(shell go env GOPATH 2>/dev/null)/bin
endif

# Make go-installed tools (e.g. wails) visible to every recipe.
ifneq ($(strip $(GOBIN_DIR)),)
export PATH := $(PATH)$(PATHSEP)$(GOBIN_DIR)
endif

# ---------------------------------------------------------------------------
# Version string: YYYYMMDD-<6-char hash>.  Falls back to "dev".
# ---------------------------------------------------------------------------
ifeq ($(OS),Windows_NT)
_RAW_VER := $(shell git log -1 --format=%cd-%h --date=format:%Y%m%d --abbrev=6 2>NUL)
else
_RAW_VER := $(shell git log -1 --format=%cd-%h --date=format:%Y%m%d --abbrev=6 2>/dev/null)
endif
ifeq ($(strip $(_RAW_VER)),)
VERSION := dev
else
VERSION := $(_RAW_VER)
endif
LDFLAGS := -X main.AppVersion=$(VERSION)

# ---------------------------------------------------------------------------
# Default target
# ---------------------------------------------------------------------------
help:
	@echo Atropos Wails Build Targets
	@echo ==============================
	@echo make setup      - Install Wails CLI and npm dependencies
	@echo make dev        - Run development server (hot-reload)
	@echo make build      - Build frontend + production binary
	@echo make debug      - Build with Wails in debug mode (-debug)
	@echo make frontend   - Build frontend only
	@echo make test       - Run Go tests
	@echo make clean      - Remove build artifacts
	@echo.
	@echo Current version: $(VERSION)

# ---------------------------------------------------------------------------
# Prereq check — separate implementations for Windows (cmd) and Unix (sh).
# ---------------------------------------------------------------------------
ifeq ($(OS),Windows_NT)
ensure-prereqs:
	@echo Checking build prerequisites...
	@where go >$(NULL) 2>&1 || (echo ERROR: 'go' not found. Install Go from https://go.dev/dl/ & exit /b 1)
	@where npm >$(NULL) 2>&1 || (echo ERROR: 'npm' not found. Install Node.js from https://nodejs.org/ & exit /b 1)
	@where wails >$(NULL) 2>&1 || (echo 'wails' CLI not found. Installing via go install... & go install github.com/wailsapp/wails/v2/cmd/wails@latest)
	@where wails >$(NULL) 2>&1 || (echo ERROR: Automatic wails install failed. Run: go install github.com/wailsapp/wails/v2/cmd/wails@latest & exit /b 1)
	@echo Prerequisites OK
else
ensure-prereqs:
	@echo "Checking build prerequisites..."
	@command -v go >/dev/null 2>&1 || { echo "Error: 'go' not found. Install Go (https://go.dev/dl/)."; exit 1; }
	@command -v npm >/dev/null 2>&1 || { echo "Error: 'npm' not found. Install Node.js (https://nodejs.org/)."; exit 1; }
	@command -v wails >/dev/null 2>&1 || ( \
		echo "'wails' CLI not found. Attempting automatic install via 'go install'..."; \
		go install github.com/wailsapp/wails/v2/cmd/wails@latest || { \
			echo "Automatic 'wails' install failed."; \
			echo "On macOS you can try: 'brew tap wailsapp/wails && brew install wails'"; \
			echo "Or install manually: 'go install github.com/wailsapp/wails/v2/cmd/wails@latest'"; \
			exit 1; \
		} \
	)
	@echo "Prerequisites OK"
endif

# ---------------------------------------------------------------------------
# Main targets
# ---------------------------------------------------------------------------

setup: ensure-prereqs
	cd frontend && npm install

dev:
	wails dev -ldflags "$(LDFLAGS)"

frontend:
	cd frontend && npm run build

build: ensure-prereqs frontend
	wails build -o $(EXE_NAME) -ldflags "$(LDFLAGS)"

debug: ensure-prereqs frontend
	wails build -o $(EXE_NAME) -ldflags "$(LDFLAGS)" -debug

test:
	go test ./... -count=1

ifeq ($(OS),Windows_NT)
clean:
	-rd /s /q frontend\dist 2>$(NULL)
	-rd /s /q build\bin 2>$(NULL)
	go clean
else
clean:
	-rm -rf frontend/dist
	-rm -rf build/bin
	go clean
endif
