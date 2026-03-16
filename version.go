package main

// AppVersion is injected at build time via -ldflags:
//
//	wails build -ldflags "-X main.AppVersion=20260316-a1b2c3"
//
// Falls back to "dev" for local `wails dev` / `go run` sessions where no
// ldflags are passed.
var AppVersion = "dev"

// AppBaseTitle returns the window title prefix used throughout the app.
// Format: "Atropos YYYYMMDD-xxxxxx"  (or "Atropos dev" locally)
func AppBaseTitle() string {
	return "Atropos " + AppVersion
}
