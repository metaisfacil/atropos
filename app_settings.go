package main

import (
	"fmt"
	"image/color"
	"strings"
)

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
