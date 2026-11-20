.PHONY: build dev clean test vet winres help ensure-prereqs debug

# ---------------------------------------------------------------------------
# Platform detection
# ---------------------------------------------------------------------------
ifeq ($(OS),Windows_NT)
EXE_NAME := atropos.exe
NULL := NUL
else
EXE_NAME := atropos
NULL := /dev/null
endif

# ---------------------------------------------------------------------------
# Version string: YYYYMMDD-<6-char hash>.  Falls back to "dev".
# ---------------------------------------------------------------------------
ifeq ($(OS),Windows_NT)
_RAW_VER := $(shell git log -1 --format=%cd-%h --date=format:%Y%m%d --abbrev=6)
else
_RAW_VER := $(shell git log -1 --format=%cd-%h --date=format:%Y%m%d --abbrev=6 2>/dev/null)
endif
ifeq ($(strip $(_RAW_VER)),)
VERSION := dev
else
VERSION := $(_RAW_VER)
endif
LDFLAGS := -X main.AppVersion=$(VERSION)
ifeq ($(OS),Windows_NT)
BUILD_LDFLAGS := -H=windowsgui $(LDFLAGS)
else
BUILD_LDFLAGS := $(LDFLAGS)
endif

# ---------------------------------------------------------------------------
# Default target
# ---------------------------------------------------------------------------
help:
	@echo Atropos Native Go / Gio Build Targets
	@echo ========================================
	@echo make dev        - Run in debug mode with terminal logging
	@echo make build      - Build production binary in build/bin/
	@echo make debug      - Build debug binary (keeps console attached on Windows)
	@echo make test       - Run Go tests
	@echo make vet        - Run go vet
	@echo make winres     - Regenerate Windows icon/manifest resource (.syso)
	@echo make clean      - Remove build artifacts
	@echo.
	@echo Current version: $(VERSION)

ifeq ($(OS),Windows_NT)
ensure-prereqs:
	@where go >$(NULL) 2>&1 || (echo ERROR: 'go' not found. Install Go from https://go.dev/dl/ & exit /b 1)
else
ensure-prereqs:
	@command -v go >/dev/null 2>&1 || { echo "Error: 'go' not found. Install Go (https://go.dev/dl/)."; exit 1; }
endif

ifeq ($(OS),Windows_NT)
dev: ensure-prereqs
	set "ATROPOS_DEBUG=1" && go run -ldflags "$(LDFLAGS)" .

build: ensure-prereqs rsrc_windows_amd64.syso
	@if not exist build\bin mkdir build\bin
	go build -ldflags "$(BUILD_LDFLAGS)" -o build\bin\$(EXE_NAME) .

debug: ensure-prereqs rsrc_windows_amd64.syso
	@if not exist build\bin mkdir build\bin
	go build -ldflags "$(LDFLAGS)" -o build\bin\$(EXE_NAME) .

clean:
	-rd /s /q build\bin 2>$(NULL)
	-if exist rsrc_windows_amd64.syso del /q rsrc_windows_amd64.syso
	go clean
else
dev: ensure-prereqs
	ATROPOS_DEBUG=1 go run -ldflags "$(LDFLAGS)" .

build: ensure-prereqs
	mkdir -p build/bin
	go build -ldflags "$(BUILD_LDFLAGS)" -o build/bin/$(EXE_NAME) .

debug: ensure-prereqs
	mkdir -p build/bin
	go build -ldflags "$(LDFLAGS)" -o build/bin/$(EXE_NAME) .

clean:
	-rm -rf build/bin
	go clean
endif

test:
	go test ./... -count=1

vet:
	go vet ./...

rsrc_windows_amd64.syso: build/windows/atropos.rc build/windows/atropos.manifest build/windows/icon.ico
	windres -F pe-x86-64 -O coff -I build/windows -i build/windows/atropos.rc -o rsrc_windows_amd64.syso

winres:
	windres -F pe-x86-64 -O coff -I build/windows -i build/windows/atropos.rc -o rsrc_windows_amd64.syso
