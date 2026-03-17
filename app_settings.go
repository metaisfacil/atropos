package main

// TouchupSettings holds the configuration for the touch-up backend.
type TouchupSettings struct {
	Backend    string `json:"backend"`
	IOPaintURL string `json:"iopaintUrl"`
}

// GetTouchupSettings returns the current touch-up backend settings.
func (a *App) GetTouchupSettings() TouchupSettings {
	return TouchupSettings{
		Backend:    a.touchupBackend,
		IOPaintURL: a.iopaintURL,
	}
}

// SetTouchupSettings updates the touch-up backend settings.
func (a *App) SetTouchupSettings(settings TouchupSettings) {
	if settings.Backend == "iopaint" || settings.Backend == "patchmatch" {
		a.touchupBackend = settings.Backend
	}
	if settings.IOPaintURL != "" {
		a.iopaintURL = settings.IOPaintURL
	}
	a.logf("SetTouchupSettings: backend=%q url=%q", a.touchupBackend, a.iopaintURL)
}
