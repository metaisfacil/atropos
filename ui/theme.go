package ui

import (
	"image/color"

	"gioui.org/font/gofont"
	"gioui.org/text"
	"gioui.org/unit"
	"gioui.org/widget/material"
)

var (
	colCanvasBg      = color.NRGBA{R: 30, G: 30, B: 30, A: 255}    // #1e1e1e
	colSidebarBg     = color.NRGBA{R: 37, G: 37, B: 38, A: 255}    // #252526
	colSurface       = color.NRGBA{R: 45, G: 45, B: 48, A: 255}    // #2d2d30
	colSurfaceHover  = color.NRGBA{R: 56, G: 56, B: 60, A: 255}    // #38383c
	colBorder        = color.NRGBA{R: 62, G: 62, B: 66, A: 255}    // #3e3e42
	colBorderLight   = color.NRGBA{R: 85, G: 85, B: 85, A: 255}    // #555555
	colText          = color.NRGBA{R: 224, G: 224, B: 224, A: 255} // #e0e0e0
	colTextMuted     = color.NRGBA{R: 133, G: 133, B: 133, A: 255} // #858585
	colTextSubtle    = color.NRGBA{R: 170, G: 170, B: 170, A: 255} // #aaaaaa
	colTextHint      = color.NRGBA{R: 184, G: 184, B: 184, A: 255} // #b8b8b8
	colWhite         = color.NRGBA{R: 255, G: 255, B: 255, A: 255}
	colPrimary       = color.NRGBA{R: 0, G: 122, B: 204, A: 255}   // #007acc
	colPrimaryHover  = color.NRGBA{R: 0, G: 90, B: 158, A: 255}    // #005a9e
	colPrimaryActive = color.NRGBA{R: 0, G: 69, B: 120, A: 255}    // #004578
	colAdjust        = color.NRGBA{R: 0, G: 135, B: 184, A: 255}   // #0087b8
	colAdjustHover   = color.NRGBA{R: 0, G: 111, B: 148, A: 255}   // #006f94
	colAdjustActive  = color.NRGBA{R: 0, G: 86, B: 111, A: 255}    // #00566f
	colLoad          = color.NRGBA{R: 16, G: 121, B: 63, A: 255}   // #10793f
	colLoadHover     = color.NRGBA{R: 14, G: 92, B: 46, A: 255}    // #0e5c2e
	colSave          = color.NRGBA{R: 197, G: 92, B: 17, A: 255}   // #c55c11
	colSaveHover     = color.NRGBA{R: 168, G: 74, B: 13, A: 255}   // #a84a0d
	colSkip          = color.NRGBA{R: 118, G: 69, B: 127, A: 255}  // #76457f
	colSkipHover     = color.NRGBA{R: 99, G: 55, B: 106, A: 255}   // #63376a
	colSkipActive    = color.NRGBA{R: 80, G: 45, B: 87, A: 255}    // #502d57
	colRecrop        = color.NRGBA{R: 61, G: 87, B: 120, A: 255}   // #3d5778
	colRecropHover   = color.NRGBA{R: 48, G: 69, B: 98, A: 255}    // #304562
	colRecropActive  = color.NRGBA{R: 37, G: 54, B: 77, A: 255}    // #25364d
	colDanger        = color.NRGBA{R: 196, G: 43, B: 28, A: 255}   // #c42b1c
	colDangerHover   = color.NRGBA{R: 163, G: 36, B: 23, A: 255}   // #a32417
	colDangerActive  = color.NRGBA{R: 139, G: 30, B: 19, A: 255}   // #8b1e13
	colSecondary     = color.NRGBA{R: 62, G: 62, B: 66, A: 255}    // #3e3e42
	colSecondaryHov  = color.NRGBA{R: 80, G: 80, B: 84, A: 255}    // #505054
	colModeHover     = color.NRGBA{R: 69, G: 69, B: 71, A: 255}    // #454547
	colCheckerDark   = color.NRGBA{R: 37, G: 37, B: 37, A: 255}    // #252525
	colCheckerLight  = color.NRGBA{R: 48, G: 48, B: 48, A: 255}    // #303030
	colBackdrop      = color.NRGBA{R: 0, G: 0, B: 0, A: 140}
	colBackdropDark  = color.NRGBA{R: 0, G: 0, B: 0, A: 184}
	colLoadingOpaque = color.NRGBA{R: 30, G: 30, B: 30, A: 217}
	colTooltipBg     = color.NRGBA{R: 30, G: 30, B: 34, A: 245}
	colGreen         = color.NRGBA{R: 0, G: 255, B: 0, A: 255}
	colGreenFill     = color.NRGBA{R: 0, G: 255, B: 0, A: 20}
	colGreenDark     = color.NRGBA{R: 11, G: 26, B: 11, A: 255}
	colGreenLight    = color.NRGBA{R: 185, G: 255, B: 185, A: 255}
	colYellow        = color.NRGBA{R: 255, G: 255, B: 0, A: 255}
	colAmber         = color.NRGBA{R: 231, G: 189, B: 98, A: 255}
	colDoneGreen     = color.NRGBA{R: 103, G: 211, B: 145, A: 255}
	colLinkHover     = color.NRGBA{R: 61, G: 174, B: 233, A: 255}
)

func newAtroposTheme() *material.Theme {
	th := material.NewTheme()
	th.Shaper = text.NewShaper(text.WithCollection(gofont.Collection()))
	th.Palette = material.Palette{
		Bg:         colCanvasBg,
		Fg:         colText,
		ContrastBg: colPrimary,
		ContrastFg: colWhite,
	}
	th.TextSize = unit.Sp(13)
	return th
}

func defaultSpatialKeyLabels() map[string]string {
	return map[string]string{
		"KeyW": "W",
		"KeyA": "A",
		"KeyS": "S",
		"KeyD": "D",
		"KeyQ": "Q",
		"KeyE": "E",
	}
}

func scaleAlpha(c color.NRGBA, factor float32) color.NRGBA {
	if factor >= 1 {
		return c
	}
	if factor <= 0 {
		c.A = 0
		return c
	}
	c.A = uint8(float32(c.A) * factor)
	return c
}
