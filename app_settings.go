package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// settingsFilePath returns the path to the shared settings JSON file.
func settingsFilePath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "atropos", "settings.json"), nil
}

// GetAllSettings reads the settings file and returns its contents.
// If the file does not exist the compiled-in defaults are returned with
// Initialized=false so the frontend can migrate any legacy localStorage
// values before they are lost.
func (a *App) GetAllSettings() AllSettings {
	defaults := AllSettings{
		TouchupBackend:            a.touchupBackend,
		IOPaintURL:                a.iopaintURL,
		WarpFillMode:              a.warpFillMode,
		WarpFillColor:             fmt.Sprintf("#%02x%02x%02x", a.warpFillColor.R, a.warpFillColor.G, a.warpFillColor.B),
		CropEdgePixels:            3,
		DiscCenterCutout:          a.discCenterCutout,
		DiscCutoutPercent:         a.discCutoutPercent,
		CornerMaxCorners:          500,
		CornerMinDistance:         100,
		CornerSettingsVersion:     1,
		AutoCornerParams:          false,
		CloseAfterSave:            false,
		PostSaveEnabled:           false,
		PostSaveCommand:           "",
		TouchupRemainsActive:      true,
		StraightEdgeRemainsActive: true,
		AutoDetectOnModeSwitch:    true,
		Initialized:               false,
	}

	path, err := settingsFilePath()
	if err != nil {
		return defaults
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return defaults // file not yet created; Initialized stays false
	}
	var s AllSettings
	if err := json.Unmarshal(data, &s); err != nil {
		a.logf("GetAllSettings: parse error: %v", err)
		return defaults
	}
	s = sanitizeSettings(s)
	s.Initialized = true
	a.logf("GetAllSettings: loaded from %s", path)
	return s
}

// sanitizeSettings replaces any field that fails validation with its default
// value.  This is run both when reading from disk and before writing, so
// neither the frontend nor the file ever contains an invalid value.
func sanitizeSettings(s AllSettings) AllSettings {
	if s.TouchupBackend != "iopaint" && s.TouchupBackend != "patchmatch" {
		s.TouchupBackend = "patchmatch"
	}
	if s.IOPaintURL == "" {
		s.IOPaintURL = "http://127.0.0.1:8086/"
	}
	if s.WarpFillMode != "clamp" && s.WarpFillMode != "fill" && s.WarpFillMode != "outpaint" {
		s.WarpFillMode = "clamp"
	}
	if _, err := parseHexColor(s.WarpFillColor); err != nil {
		s.WarpFillColor = "#ffffff"
	}
	if s.CropEdgePixels < 1 || s.CropEdgePixels > 1000 {
		s.CropEdgePixels = 3
	}
	if s.DiscCutoutPercent < 0 || s.DiscCutoutPercent > 50 {
		s.DiscCutoutPercent = 11
	}
	if s.CornerMaxCorners < 1 || s.CornerMaxCorners > 1000 {
		s.CornerMaxCorners = 500
	}
	if s.CornerMinDistance < 1 || s.CornerMinDistance > 200 {
		s.CornerMinDistance = 100
	}
	return s
}

// SaveAllSettings writes the provided settings to the shared JSON file and
// also applies the backend-relevant fields to the in-memory App state so they
// take effect immediately in this instance.
func (a *App) SaveAllSettings(s AllSettings) error {
	s = sanitizeSettings(s)

	// Disc parameter edits branch redo; history synchronization supplies the
	// parameters already restored in the backend and keeps redo intact.
	if a.discCenterCutout != s.DiscCenterCutout || a.discCutoutPercent != s.DiscCutoutPercent {
		a.redoStack = nil
	}

	// Apply backend-relevant fields immediately.
	a.touchupBackend = s.TouchupBackend
	a.iopaintURL = s.IOPaintURL
	a.warpFillMode = s.WarpFillMode
	if c, err := parseHexColor(s.WarpFillColor); err == nil {
		a.warpFillColor = c
	}
	a.discCenterCutout = s.DiscCenterCutout
	a.discCutoutPercent = s.DiscCutoutPercent

	// Persist to file.
	path, err := settingsFilePath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	s.AppVersion = AppVersion
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return err
	}
	a.logf("SaveAllSettings: written to %s", path)
	return nil
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

// GetWarpSettings returns the current warp out-of-bounds fill settings.
func (a *App) GetWarpSettings() WarpSettings {
	c := a.warpFillColor
	return WarpSettings{
		FillMode:  a.warpFillMode,
		FillColor: fmt.Sprintf("#%02x%02x%02x", c.R, c.G, c.B),
	}
}

// SetWarpSettings updates the warp out-of-bounds fill settings.
func (a *App) SetWarpSettings(settings WarpSettings) {
	if settings.FillMode == "clamp" || settings.FillMode == "fill" || settings.FillMode == "outpaint" {
		a.warpFillMode = settings.FillMode
	}
	if settings.FillColor != "" {
		if c, err := parseHexColor(settings.FillColor); err == nil {
			a.warpFillColor = c
		}
	}
	a.logf("SetWarpSettings: fillMode=%q fillColor=%q", a.warpFillMode, settings.FillColor)
}

// GetDiscSettings returns the current disc mode settings.
func (a *App) GetDiscSettings() DiscSettings {
	return DiscSettings{
		CenterCutout:  a.discCenterCutout,
		CutoutPercent: a.discCutoutPercent,
	}
}

// SetDiscSettings updates the disc mode settings and re-renders any active disc.
func (a *App) SetDiscSettings(settings DiscSettings) (*ProcessResult, error) {
	a.redoStack = nil
	a.discCenterCutout = settings.CenterCutout
	if settings.CutoutPercent >= 0 && settings.CutoutPercent <= 50 {
		a.discCutoutPercent = settings.CutoutPercent
	}
	a.logf("SetDiscSettings: centerCutout=%v cutoutPercent=%d", a.discCenterCutout, a.discCutoutPercent)
	if a.discRadius > 0 {
		return a.redrawDisc()
	}
	return &ProcessResult{}, nil
}
