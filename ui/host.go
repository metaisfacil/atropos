package ui

import (
	"errors"
	"image/color"
	"path/filepath"
	"sync/atomic"

	"gioui.org/app"
	"gioui.org/io/system"
	"github.com/ncruces/zenity"
)

type gioHost struct {
	win           *app.Window
	dispatch      func(func())
	onTouchupDone func(touchUpDoneEvent)
	onQuit        func()
	hwnd          atomic.Uintptr
}

func newGioHost(win *app.Window, dispatch func(func()), onTouchupDone func(touchUpDoneEvent), onQuit func()) *gioHost {
	return &gioHost{
		win:           win,
		dispatch:      dispatch,
		onTouchupDone: onTouchupDone,
		onQuit:        onQuit,
	}
}

func (h *gioHost) SetWindowTitle(title string) {
	if h == nil || h.win == nil {
		return
	}
	h.win.Option(app.Title(title))
}

func (h *gioHost) Quit() {
	if h == nil {
		return
	}
	if h.onQuit != nil {
		if h.dispatch != nil {
			h.dispatch(h.onQuit)
		} else {
			h.onQuit()
		}
		return
	}
	if h.win != nil {
		h.win.Perform(system.ActionClose)
	}
}

func (h *gioHost) OpenFileDialog(title, defaultDir, defaultName string, filters []FileFilter) (string, error) {
	opts := []zenity.Option{zenity.Title(title)}
	if opt, ok := attachWindowOption(h.hwnd.Load()); ok {
		opts = append(opts, opt)
	}
	if fn := dialogFilename(defaultDir, defaultName); fn != "" {
		opts = append(opts, zenity.Filename(fn))
	}
	if zf := toZenityFilters(filters); len(zf) > 0 {
		opts = append(opts, zf)
	}
	path, err := zenity.SelectFile(opts...)
	if errors.Is(err, zenity.ErrCanceled) {
		return "", nil
	}
	return path, err
}

func (h *gioHost) OpenMultipleFilesDialog(title, defaultDir string, filters []FileFilter) ([]string, error) {
	opts := []zenity.Option{zenity.Title(title)}
	if opt, ok := attachWindowOption(h.hwnd.Load()); ok {
		opts = append(opts, opt)
	}
	if fn := dialogFilename(defaultDir, ""); fn != "" {
		opts = append(opts, zenity.Filename(fn))
	}
	if zf := toZenityFilters(filters); len(zf) > 0 {
		opts = append(opts, zf)
	}
	paths, err := zenity.SelectFileMultiple(opts...)
	if errors.Is(err, zenity.ErrCanceled) {
		return nil, nil
	}
	return paths, err
}

func (h *gioHost) SaveFileDialog(title, defaultDir, defaultName string, filters []FileFilter) (string, error) {
	opts := []zenity.Option{
		zenity.Title(title),
		zenity.ConfirmOverwrite(),
	}
	if opt, ok := attachWindowOption(h.hwnd.Load()); ok {
		opts = append(opts, opt)
	}
	if fn := dialogFilename(defaultDir, defaultName); fn != "" {
		opts = append(opts, zenity.Filename(fn))
	}
	if zf := toZenityFilters(filters); len(zf) > 0 {
		opts = append(opts, zf)
	}
	path, err := zenity.SelectFileSave(opts...)
	if errors.Is(err, zenity.ErrCanceled) {
		return "", nil
	}
	return path, err
}

func (h *gioHost) SelectColorDialog(initial color.NRGBA) (color.NRGBA, bool, error) {
	opts := []zenity.Option{
		zenity.Title("Choose Fill Colour"),
		zenity.Color(initial),
	}
	if opt, ok := attachWindowOption(h.hwnd.Load()); ok {
		opts = append(opts, opt)
	}
	c, err := zenity.SelectColor(opts...)
	if errors.Is(err, zenity.ErrCanceled) {
		return color.NRGBA{}, false, nil
	}
	if err != nil {
		return color.NRGBA{}, false, err
	}
	nrgba := color.NRGBAModel.Convert(c).(color.NRGBA)
	nrgba.A = 255
	return nrgba, true, nil
}

func (h *gioHost) OpenURL(url string) error {
	return openSystemURL(url)
}

func (h *gioHost) EmitTouchupDone(ev touchUpDoneEvent) {
	if h == nil || h.onTouchupDone == nil {
		return
	}
	if h.dispatch != nil {
		h.dispatch(func() {
			h.onTouchupDone(ev)
		})
		return
	}
	h.onTouchupDone(ev)
}

func toZenityFilters(filters []FileFilter) zenity.FileFilters {
	if len(filters) == 0 {
		return nil
	}
	out := make(zenity.FileFilters, 0, len(filters))
	for _, f := range filters {
		out = append(out, zenity.FileFilter{
			Name:     f.DisplayName,
			Patterns: append([]string(nil), f.Patterns...),
			CaseFold: true,
		})
	}
	return out
}

func dialogFilename(defaultDir, defaultName string) string {
	if defaultDir != "" && defaultName != "" {
		return filepath.Join(defaultDir, defaultName)
	}
	if defaultDir != "" {
		return defaultDir + string(filepath.Separator)
	}
	return defaultName
}
