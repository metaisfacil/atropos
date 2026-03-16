.PHONY: build dev clean setup frontend test help

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

# Default target
help:
	@echo Atropos Wails Build Targets
	@echo ==============================
	@echo make setup      - Install Wails CLI and npm dependencies
	@echo make dev        - Run development server (hot-reload)
	@echo make build      - Build frontend + production binary
	@echo make frontend   - Build frontend only
	@echo make test       - Run Go tests
	@echo make clean      - Remove build artifacts

# Install dependencies
setup:
	go install github.com/wailsapp/wails/v2/cmd/wails@latest
	cd frontend && npm install

# Development mode (Wails handles frontend dev server)
dev:
	wails dev

# Build frontend (React/Vite)
frontend:
	cd frontend && npm run build

# Production build: frontend first, then Wails embeds dist/ into the binary
build: frontend
	wails build -o $(EXE_NAME)

# Run Go tests
test:
	go test ./... -count=1

# Remove build artifacts
clean:
	-rm -rf frontend/dist
	-rm -rf build/bin
	go clean
