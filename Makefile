.PHONY: build dev clean setup frontend test help ensure-prereqs debug

# Ensure Go's GOPATH/bin is visible to Make recipes so `go install`-ed
# tools like `wails` are found during the build.
GOBIN_DIR := $(shell go env GOPATH 2>/dev/null)/bin
ifneq ($(strip $(GOBIN_DIR)),)
export PATH := $(PATH):$(GOBIN_DIR)
endif

# Detect OS for platform-specific output name
ifeq ($(OS),Windows_NT)
	EXE_NAME := atropos.exe
else
	UNAME_S := $(shell uname -s 2>/dev/null)
	ifeq ($(UNAME_S),Windows_NT)
		EXE_NAME := atropos.exe
	else
		EXE_NAME := atropos
	endif
endif
# Fallback: if EXE_NAME is still empty, default to atropos.exe (for Windows shells where above logic fails)
ifeq ($(strip $(EXE_NAME)),)
EXE_NAME := atropos.exe
endif

# Version string: YYYYMMDD-<6-char hash> from the last commit.
# Falls back to "dev" when git is unavailable or the repo has no commits.
VERSION := $(shell git log -1 --format=%cd-%h --date=format:%Y%m%d --abbrev=6 2>/dev/null || echo dev)
LDFLAGS := -X main.AppVersion=$(VERSION)

# Default target
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
	@echo ""
	@echo "Current version: $(VERSION)"

# Install dependencies
setup:
	$(MAKE) ensure-prereqs
	cd frontend && npm install

# Development mode — version string is baked in at dev-server startup too
dev:
	wails dev -ldflags "$(LDFLAGS)"

# Build frontend (React/Vite)
frontend:
	cd frontend && npm run build

# Ensure required tools are present before attempting a build
# Production build: frontend first, then Wails embeds dist/ into the binary
build: ensure-prereqs frontend
	wails build -o $(EXE_NAME) -ldflags "$(LDFLAGS)"

# Debug build: same as `build` but enables Wails debug mode
.PHONY: debug
debug: ensure-prereqs frontend
	wails build -o $(EXE_NAME) -ldflags "$(LDFLAGS)" -debug

.PHONY: ensure-prereqs
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

# Run Go tests
test:
	go test ./... -count=1

# Remove build artifacts
clean:
	-rm -rf frontend/dist
	-rm -rf build/bin
	go clean
