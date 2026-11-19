package main

// SetHost attaches the UI host bridge to the application backend.
func (a *App) SetHost(h AppHost) {
	a.host = h
}

func (a *App) setWindowTitle(title string) {
	if a.host != nil {
		a.host.SetWindowTitle(title)
	}
}

func (a *App) quitApp() {
	if a.host != nil {
		a.host.Quit()
	}
}

func (a *App) openFileDialog(title, defaultDir, defaultName string, filters []FileFilter) (string, error) {
	if a.host == nil {
		return "", nil
	}
	return a.host.OpenFileDialog(title, defaultDir, defaultName, filters)
}

func (a *App) openMultipleFilesDialog(title, defaultDir string, filters []FileFilter) ([]string, error) {
	if a.host == nil {
		return nil, nil
	}
	return a.host.OpenMultipleFilesDialog(title, defaultDir, filters)
}

func (a *App) saveFileDialog(title, defaultDir, defaultName string, filters []FileFilter) (string, error) {
	if a.host == nil {
		return "", nil
	}
	return a.host.SaveFileDialog(title, defaultDir, defaultName, filters)
}

func (a *App) openURL(url string) error {
	if a.host == nil {
		return nil
	}
	return a.host.OpenURL(url)
}

// OpenURL opens a URL using the attached UI host.
func (a *App) OpenURL(url string) error {
	return a.openURL(url)
}

func (a *App) emitTouchupDone(ev touchUpDoneEvent) {
	if a.host != nil {
		a.host.EmitTouchupDone(ev)
	}
}
