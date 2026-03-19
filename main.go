package main

import (
	"embed"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
)

//go:embed all:frontend/dist/*
var assets embed.FS

func main() {
	// Parse flags: --debug, --corners, --disc, --lines, --normal, --post-save, and positional image path
	debug := false
	launchMode := ""
	launchFile := ""
	postSave := ""
	postSaveExit := false
	for i := 1; i < len(os.Args); i++ {
		arg := os.Args[i]
		switch {
		case arg == "--debug" || arg == "-debug":
			debug = true
		case arg == "--corners":
			launchMode = "corner"
		case arg == "--disc":
			launchMode = "disc"
		case arg == "--lines":
			launchMode = "line"
		case arg == "--normal":
			launchMode = "normal"
		case strings.HasPrefix(arg, "--post-save="):
			postSave = strings.TrimPrefix(arg, "--post-save=")
		case strings.HasPrefix(arg, "--post-save-exit="):
			v := strings.TrimPrefix(arg, "--post-save-exit=")
			if v == "1" || strings.EqualFold(v, "true") || strings.EqualFold(v, "yes") {
				postSaveExit = true
			}
		case arg == "--post-save":
			// Consume next arg as the command, if present
			if i+1 < len(os.Args) {
				i++
				postSave = os.Args[i]
			}
		case arg == "--post-save-exit":
			postSaveExit = true
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
	app.postSaveCmd = postSave
	app.postSaveExit = postSaveExit

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
