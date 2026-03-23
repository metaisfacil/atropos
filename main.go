package main

import (
	"context"
	"embed"
	"log"
	"os"
	"strings"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/windows"
	"github.com/wailsapp/wails/v2/pkg/runtime"
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
		logger = log.New(os.Stderr, "[Atropos] ", log.Ldate|log.Ltime|log.Lmicroseconds)
		cwd, _ := os.Getwd()
		logger.Println("=== Atropos debug session started ===")
		logger.Printf("CWD: %s", cwd)
		logger.Printf("Args: %v", os.Args)
	}

	// Create an instance of the app structure
	app := NewApp()
	app.logger = logger
	app.launchFilePath = launchFile
	app.launchMode = launchMode
	app.postSaveCmd = postSave
	app.postSaveExit = postSaveExit

	// Resolve a WebView2 user data directory that is not already locked by
	// another running instance.  The stable per-user path is preferred so
	// that localStorage preferences survive across launches; a PID-keyed
	// temp path is used as a fallback when a second (or further) instance
	// is opened.
	dataPath := webviewUserDataPath()

	// Create application with options
	err := wails.Run(&options.App{
		Title:     AppBaseTitle(),
		Width:     1200,
		Height:    900,
		MinWidth:  1080,
		MinHeight: 820,
		AssetServer: &assetserver.Options{
			Assets: assets,
		},
		BackgroundColour: &options.RGBA{R: 27, G: 27, B: 27, A: 1},
		DragAndDrop: &options.DragAndDrop{
			EnableFileDrop:     true,
			DisableWebViewDrop: true,
		},
		Windows: &windows.Options{
			WebviewUserDataPath: dataPath,
		},
		OnStartup:  app.startup,
		OnShutdown: app.shutdown,
		OnBeforeClose: func(ctx context.Context) (prevent bool) {
			if app.closeConfirmed {
				return false
			}
			// Ask frontend path for confirmation.
			runtime.EventsEmit(ctx, "app-close-requested")
			return true
		},
		Bind: []interface{}{
			app,
		},
	})

	if err != nil {
		println("Error:", err.Error())
	}
}
