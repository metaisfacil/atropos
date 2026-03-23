package main

import (
	"encoding/json"
	"fmt"
	"image/color"
	"os"
	"path/filepath"
	"strings"
)

// AllSettings is the complete set of user-facing persistent settings.
// It is serialised to / deserialised from the per-user settings file so that
// every application instance shares the same values regardless of which
// WebView2 user data directory it is using.
type AllSettings struct {
	// Touch-up
	TouchupBackend string `json:"touchupBackend"`
	IOPaintURL     string `json:"iopaintUrl"`
	// Warp fill
	WarpFillMode  string `json:"warpFillMode"`
	WarpFillColor string `json:"warpFillColor"`
	// Disc
	DiscCenterCutout  bool `json:"discCenterCutout"`
	DiscCutoutPercent int  `json:"discCutoutPercent"`
	// Behaviour flags (frontend-only values persisted here for sharing)
	AutoCornerParams          bool   `json:"autoCornerParams"`
	CloseAfterSave            bool   `json:"closeAfterSave"`
	PostSaveEnabled           bool   `json:"postSaveEnabled"`
	PostSaveCommand           string `json:"postSaveCommand"`
	TouchupRemainsActive      bool   `json:"touchupRemainsActive"`
	StraightEdgeRemainsActive bool   `json:"straightEdgeRemainsActive"`
	AutoDetectOnModeSwitch    bool   `json:"autoDetectOnModeSwitch"`
	// AppVersion is the build version string of the last app instance that
	// successfully wrote this file.  Useful for troubleshooting.
	AppVersion string `json:"appVersion"`
	// Initialized is false when the settings file did not exist on disk,
	// meaning this is the first launch of a version that uses file-based
	// settings.  The frontend uses this flag to migrate any values it finds
	// in localStorage (written by older versions) before they are lost.
	// The field is never written to the JSON file itself (json:"-").
	Initialized bool `json:"-"`
}

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
		DiscCenterCutout:          a.discCenterCutout,
		DiscCutoutPercent:         a.discCutoutPercent,
		AutoCornerParams:          true,
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
	if s.DiscCutoutPercent < 0 || s.DiscCutoutPercent > 50 {
		s.DiscCutoutPercent = 11
	}
	return s
}

// SaveAllSettings writes the provided settings to the shared JSON file and
// also applies the backend-relevant fields to the in-memory App state so they
// take effect immediately in this instance.
func (a *App) SaveAllSettings(s AllSettings) error {
	s = sanitizeSettings(s)

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

// WarpSettings holds configuration for how out-of-bounds regions produced by
// perspective warping are handled.
type WarpSettings struct {
	// FillMode is "clamp", "fill" (solid colour), or "outpaint" (PatchMatch).
	FillMode string `json:"fillMode"`
	// FillColor is a CSS hex colour string (e.g. "#ffffff") used when FillMode=="fill".
	FillColor string `json:"fillColor"`
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

// DiscSettings holds configuration for disc-mode rendering.
type DiscSettings struct {
	// CenterCutout controls whether a circular hole is punched out at the
	// centre, filling it with the background colour so the eyedropper can
	// affect that region too.
	CenterCutout bool `json:"centerCutout"`
	// CutoutPercent is the cutout diameter as a percentage of the disc
	// diameter (1–50). Only used when CenterCutout is true.
	CutoutPercent int `json:"cutoutPercent"`
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

// parseHexColor parses a CSS hex colour string ("#rrggbb" or "#rgb") into color.NRGBA.
func parseHexColor(s string) (color.NRGBA, error) {
	s = strings.TrimPrefix(s, "#")
	if len(s) == 3 {
		s = string([]byte{s[0], s[0], s[1], s[1], s[2], s[2]})
	}
	if len(s) != 6 {
		return color.NRGBA{}, fmt.Errorf("invalid hex color %q", s)
	}
	var r, g, b uint8
	_, err := fmt.Sscanf(s, "%02x%02x%02x", &r, &g, &b)
	if err != nil {
		return color.NRGBA{}, err
	}
	return color.NRGBA{R: r, G: g, B: b, A: 255}, nil
}
