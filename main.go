package main

import (
	"log"
	"os"
	"strings"

	"atropos/ui"

	"gioui.org/app"
	"gioui.org/unit"
)

func parseCLIArgs(args []string) (debug bool, launchMode, launchFile, postSave string, postSaveExit bool) {
	if os.Getenv("ATROPOS_DEBUG") == "1" || strings.EqualFold(os.Getenv("ATROPOS_DEBUG"), "true") {
		debug = true
	}
	for i := 0; i < len(args); i++ {
		arg := args[i]
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
			if i+1 < len(args) {
				i++
				postSave = args[i]
			}
		case arg == "--post-save-exit":
			postSaveExit = true
		default:
			if len(arg) > 0 && arg[0] != '-' {
				launchFile = arg
			}
		}
	}
	return debug, launchMode, launchFile, postSave, postSaveExit
}

func main() {
	dpiMode := ui.InitPlatformDPI()
	debug, launchMode, launchFile, postSave, postSaveExit := parseCLIArgs(os.Args[1:])
	var logger *log.Logger
	if debug {
		logger = log.New(os.Stderr, "[Atropos] ", log.Ldate|log.Ltime|log.Lmicroseconds)
		cwd, _ := os.Getwd()
		logger.Println("=== Atropos debug session started ===")
		logger.Printf("CWD: %s", cwd)
		logger.Printf("Args: %v", os.Args)
		logger.Printf("DPI awareness: %s", dpiMode)
	}

	application := NewApp()
	application.logger = logger
	application.launchFilePath = launchFile
	application.launchMode = launchMode
	application.postSaveCmd = postSave
	application.postSaveExit = postSaveExit

	go func() {
		win := new(app.Window)
		win.Option(
			app.Title(AppBaseTitle()),
			app.Size(unit.Dp(1200), unit.Dp(900)),
			app.MinSize(unit.Dp(1080), unit.Dp(820)),
		)
		editor := ui.NewEditorUI(application, win, ui.Config{
			AppVersion: AppVersion,
			AppIconPNG: appIconPNG,
		})
		editor.InitOnStartup()
		if err := editor.RunWindowLoop(); err != nil {
			log.Printf("window loop error: %v", err)
		}
		os.Exit(0)
	}()

	app.Main()
}
