package main

import (
	"errors"
	"path/filepath"
	"testing"
)

type saveDialogStubHost struct {
	gotTitle       string
	gotDefaultDir  string
	gotDefaultName string
	gotFilters     []FileFilter
	returnPath     string
	returnErr      error
}

func (s *saveDialogStubHost) SetWindowTitle(string) {}
func (s *saveDialogStubHost) Quit()                 {}
func (s *saveDialogStubHost) OpenFileDialog(string, string, string, []FileFilter) (string, error) {
	return "", nil
}
func (s *saveDialogStubHost) OpenMultipleFilesDialog(string, string, []FileFilter) ([]string, error) {
	return nil, nil
}
func (s *saveDialogStubHost) SaveFileDialog(title, defaultDir, defaultName string, filters []FileFilter) (string, error) {
	s.gotTitle = title
	s.gotDefaultDir = defaultDir
	s.gotDefaultName = defaultName
	s.gotFilters = append([]FileFilter(nil), filters...)
	return s.returnPath, s.returnErr
}
func (s *saveDialogStubHost) OpenURL(string) error             { return nil }
func (s *saveDialogStubHost) EmitTouchupDone(touchUpDoneEvent) {}

func TestOpenSaveDialogDefaultNameAndExtension(t *testing.T) {
	dir := t.TempDir()

	cases := []struct {
		name           string
		loadedFilePath string
		hostReturnPath string
		hostReturnErr  error
		wantDefaultDir string
		wantDefault    string
		wantReturned   string
		wantErr        bool
	}{
		{
			name:           "no loaded file uses output.png",
			loadedFilePath: "",
			hostReturnPath: filepath.Join(dir, "output.png"),
			wantDefaultDir: "",
			wantDefault:    "output.png",
			wantReturned:   filepath.Join(dir, "output.png"),
		},
		{
			name:           "loaded jpg defaults to stem.png",
			loadedFilePath: filepath.Join(dir, "glare_removed_dead_on.jpg"),
			hostReturnPath: filepath.Join(dir, "glare_removed_dead_on.png"),
			wantDefaultDir: dir,
			wantDefault:    "glare_removed_dead_on.png",
			wantReturned:   filepath.Join(dir, "glare_removed_dead_on.png"),
		},
		{
			name:           "multi-dot stem replaces only final extension with png",
			loadedFilePath: filepath.Join(dir, "scan.v2.tif"),
			hostReturnPath: filepath.Join(dir, "scan.v2.jpg"),
			wantDefaultDir: dir,
			wantDefault:    "scan.v2.png",
			wantReturned:   filepath.Join(dir, "scan.v2.jpg"),
		},
		{
			name:           "extensionless host return gets png appended",
			loadedFilePath: filepath.Join(dir, "glare_removed_dead_on.png"),
			hostReturnPath: filepath.Join(dir, "glare_removed_dead_on"),
			wantDefaultDir: dir,
			wantDefault:    "glare_removed_dead_on.png",
			wantReturned:   filepath.Join(dir, "glare_removed_dead_on.png"),
		},
		{
			name:           "cancel returns empty path",
			loadedFilePath: filepath.Join(dir, "glare_removed_dead_on.png"),
			hostReturnPath: "",
			wantDefaultDir: dir,
			wantDefault:    "glare_removed_dead_on.png",
			wantReturned:   "",
		},
		{
			name:           "error keeps path unmodified",
			loadedFilePath: filepath.Join(dir, "glare_removed_dead_on.png"),
			hostReturnPath: filepath.Join(dir, "glare_removed_dead_on"),
			hostReturnErr:  errors.New("dialog failed"),
			wantDefaultDir: dir,
			wantDefault:    "glare_removed_dead_on.png",
			wantReturned:   filepath.Join(dir, "glare_removed_dead_on"),
			wantErr:        true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a := NewApp()
			host := &saveDialogStubHost{
				returnPath: tc.hostReturnPath,
				returnErr:  tc.hostReturnErr,
			}
			a.SetHost(host)
			a.loadedFilePath = tc.loadedFilePath

			got, err := a.OpenSaveDialog()
			if (err != nil) != tc.wantErr {
				t.Fatalf("OpenSaveDialog() err = %v, wantErr=%v", err, tc.wantErr)
			}
			if host.gotDefaultDir != tc.wantDefaultDir {
				t.Fatalf("defaultDir = %q, want %q", host.gotDefaultDir, tc.wantDefaultDir)
			}
			if host.gotDefaultName != tc.wantDefault {
				t.Fatalf("defaultName = %q, want %q", host.gotDefaultName, tc.wantDefault)
			}
			if got != tc.wantReturned {
				t.Fatalf("OpenSaveDialog() = %q, want %q", got, tc.wantReturned)
			}
		})
	}
}
