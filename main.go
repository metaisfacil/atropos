package main

import (
	"embed"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
)

//go:embed all:frontend/dist/*
var assets embed.FS

func main() {
	// Parse flags: --debug, --corners, --disc, --lines, and positional image path
	debug := false
	launchMode := ""
	launchFile := ""
	for _, arg := range os.Args[1:] {
		switch arg {
		case "--debug", "-debug":
			debug = true
		case "--corners":
			launchMode = "corner"
		case "--disc":
			launchMode = "disc"
		case "--lines":
			launchMode = "line"
		default:
			// Treat non-flag args as file path
			if len(arg) > 0 && arg[0] != '-' {
				launchFile = arg
			}
		}
	}

	// Set up debug logger if requested
	var logger *log.Logger
	if debug {
		cwd, _ := os.Getwd()
		debugDir := filepath.Join(cwd, "debug")
		if err := os.MkdirAll(debugDir, 0o755); err != nil {
			fmt.Fprintf(os.Stderr, "Failed to create debug dir: %v\n", err)
			os.Exit(1)
		}
		ts := time.Now().Format("20060102_150405")
		logPath := filepath.Join(debugDir, ts+".txt")
		f, err := os.Create(logPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed to create log file: %v\n", err)
			os.Exit(1)
		}
		// NOTE: file is intentionally never closed; it stays open for the
		// lifetime of the process and the OS reclaims it on exit.
		logger = log.New(f, "", log.Ldate|log.Ltime|log.Lmicroseconds)
		logger.Println("=== Atropos debug session started ===")
		logger.Printf("CWD: %s", cwd)
		logger.Printf("Args: %v", os.Args)
		fmt.Fprintf(os.Stderr, "[Atropos] Debug log: %s\n", logPath)
	}

	// Create an instance of the app structure
	app := NewApp()
	app.logger = logger
	app.launchFilePath = launchFile
	app.launchMode = launchMode

	// Create application with options
	err := wails.Run(&options.App{
		Title:     AppBaseTitle(),
		Width:     1200,
		Height:    900,
		MinWidth:  800,
		MinHeight: 600,
		AssetServer: &assetserver.Options{
			Assets: assets,
		},
		BackgroundColour: &options.RGBA{R: 27, G: 27, B: 27, A: 1},
		DragAndDrop: &options.DragAndDrop{
			EnableFileDrop:     true,
			DisableWebViewDrop: true,
		},
		OnStartup:  app.startup,
		OnShutdown: app.shutdown,
		Bind: []interface{}{
			app,
		},
	})

	if err != nil {
		println("Error:", err.Error())
	}
}
