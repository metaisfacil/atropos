package ui

import (
	"image"
	"image/color"

	"atropos/internal/appapi"
)

type FileFilter = appapi.FileFilter
type AppHost = appapi.AppHost

type ProcessResult = appapi.ProcessResult
type LaunchArgs = appapi.LaunchArgs

type LoadImageRequest = appapi.LoadImageRequest
type LoadImageBytesRequest = appapi.LoadImageBytesRequest
type SuggestedCornerParams = appapi.SuggestedCornerParams
type ImageInfo = appapi.ImageInfo
type SaveRequest = appapi.SaveRequest

type AdjustmentSelection = appapi.AdjustmentSelection

type CropRequest = appapi.CropRequest
type RotateRequest = appapi.RotateRequest
type ResizeRequest = appapi.ResizeRequest
type SetLevelsRequest = appapi.SetLevelsRequest
type AutoContrastRequest = appapi.AutoContrastRequest
type DescreenRequest = appapi.DescreenRequest

type CornerDetectRequest = appapi.CornerDetectRequest
type ClickCornerRequest = appapi.ClickCornerRequest
type ClickCornerResult = appapi.ClickCornerResult
type RestoreCornerOverlayRequest = appapi.RestoreCornerOverlayRequest

type DiscDrawRequest = appapi.DiscDrawRequest
type DiscRotateRequest = appapi.DiscRotateRequest
type PixelColorRequest = appapi.PixelColorRequest
type ShiftDiscRequest = appapi.ShiftDiscRequest
type DiscShiftPreview = appapi.DiscShiftPreview
type FeatherSizeRequest = appapi.FeatherSizeRequest
type FeatherRadiusRequest = appapi.FeatherRadiusRequest
type StraightEdgeRotateRequest = appapi.StraightEdgeRotateRequest

type DustRemovalRequest = appapi.DustRemovalRequest
type LineAddRequest = appapi.LineAddRequest
type NormalCropRequest = appapi.NormalCropRequest

type TouchUpPoint = appapi.TouchUpPoint
type TouchUpStrokeRequest = appapi.TouchUpStrokeRequest
type touchUpDoneEvent = appapi.TouchUpDoneEvent
type touchUpPreviewPatch = appapi.TouchUpPreviewPatch

type AllSettings = appapi.AllSettings
type TouchupSettings = appapi.TouchupSettings
type WarpSettings = appapi.WarpSettings
type DiscSettings = appapi.DiscSettings

type CalibrationLoadRequest = appapi.CalibrationLoadRequest
type CalibrationImageInfo = appapi.CalibrationImageInfo
type CalibrationPoint = appapi.CalibrationPoint
type CalibrationEntryRequest = appapi.CalibrationEntryRequest
type CalibrationSaveRequest = appapi.CalibrationSaveRequest
type CalibrationSaveResult = appapi.CalibrationSaveResult

type CompositorStitchRequest = appapi.CompositorStitchRequest
type CompositorResult = appapi.CompositorResult
type CompositorSaveRequest = appapi.CompositorSaveRequest
type CompositorMaskRequest = appapi.CompositorMaskRequest
type CompositorLoadResultRequest = appapi.CompositorLoadResultRequest

type PreviewViewportRequest = appapi.PreviewViewportRequest
type PreviewViewportResponse = appapi.PreviewViewportResponse

func parseHexColor(s string) (color.NRGBA, error) {
	return appapi.ParseHexColor(s)
}

func clamp(value, low, high int) int {
	if value < low {
		return low
	}
	if value > high {
		return high
	}
	return value
}

// Config configures metadata and embedded assets for EditorUI.
type Config struct {
	AppVersion string
	AppIconPNG []byte
}

// StateSnapshot exposes a read-only copy of UI state for integration tests.
type StateSnapshot struct {
	Mode               string
	ImageLoaded        bool
	Preview            string
	RealImageDims      image.Point
	CornerCount        int
	CornersDetected    bool
	HasCornerCache     bool
	CornerCacheWidth   int
	DiscActive         bool
	LinesProcessed     bool
	CropSkipped        bool
	CanRecrop          bool
	CanSave            bool
	UnsavedChanges     bool
	UseDescreenTool    bool
	ErrorMessage       string
	CalibrationOpen    bool
	CalibrationPreview string
	CalibrationDims    image.Point
}

// Backend defines the image-processing and host-dialog operations required by EditorUI.
type Backend interface {
	SetHost(h AppHost)
	Logf(format string, args ...interface{})
	OpenURL(url string) error

	GetAllSettings() AllSettings
	SaveAllSettings(s AllSettings) error
	GetLaunchArgs() LaunchArgs

	OpenImageDialog() (string, error)
	OpenSaveDialog() (string, error)
	LoadImage(req LoadImageRequest) (*ImageInfo, error)
	LoadImageFromClipboard() (*ImageInfo, error)
	SaveImage(req SaveRequest) (*ProcessResult, error)
	RecropImage() (*ImageInfo, error)
	RunPostSaveCommand(commandLine, savedPath string) error

	GetCleanPreview() (*ProcessResult, error)
	DetectCorners(req CornerDetectRequest) (*ProcessResult, error)
	CancelCornerDetect()
	ClickCorner(req ClickCornerRequest) (*ClickCornerResult, error)
	RestoreCornerOverlay(req RestoreCornerOverlayRequest) (*ProcessResult, error)
	UndoLastCorner() int
	ResetCorners() (*ProcessResult, error)
	SkipCrop() (*ProcessResult, error)

	DrawDisc(req DiscDrawRequest) (*ProcessResult, error)
	StraightEdgeRotate(req StraightEdgeRotateRequest) (*ProcessResult, error)
	RotateDisc(req DiscRotateRequest) (*ProcessResult, error)
	ShiftDisc(req ShiftDiscRequest) (*ProcessResult, error)
	DiscShiftPreview() (*DiscShiftPreview, error)
	GetPixelColor(req PixelColorRequest) (*ProcessResult, error)
	SetFeatherRadius(req FeatherRadiusRequest) (*ProcessResult, error)
	SetFeatherSize(req FeatherSizeRequest) (*ProcessResult, error)
	SetDiscSettings(settings DiscSettings) (*ProcessResult, error)
	ResetDisc() (*ProcessResult, error)

	AddLine(req LineAddRequest) (*ProcessResult, error)
	ProcessLines() (*ProcessResult, error)
	ClearLines() (*ProcessResult, error)
	NormalCrop(req NormalCropRequest) (*ProcessResult, error)
	ResetNormal() (*ProcessResult, error)

	Undo() (*ProcessResult, error)
	Redo() (*ProcessResult, error)
	Crop(req CropRequest) (*ProcessResult, error)
	Rotate(req RotateRequest) (*ProcessResult, error)
	ResizeImage(req ResizeRequest) (*ProcessResult, error)
	TrimBorders() (*ProcessResult, error)
	AutoContrast(req AutoContrastRequest) (*ProcessResult, error)
	Descreen(req DescreenRequest) (*ProcessResult, error)
	DustRemoval(req DustRemovalRequest) (*ProcessResult, error)
	SetLevels(req SetLevelsRequest) (*ProcessResult, error)
	CopySelectionToClipboard(selection AdjustmentSelection) (string, error)
	TouchUpApplyStrokes(request TouchUpStrokeRequest) (*ProcessResult, error)
	CancelTouchup()

	ResolvePreviewImage(previewURL string) (*image.NRGBA, bool)
	ResolveCalibrationImage(previewURL string) (*image.NRGBA, bool)

	CalibrationOpenFilesDialog() ([]string, error)
	CalibrationLoadImage(req CalibrationLoadRequest) (*CalibrationImageInfo, error)
	CalibrationClearPreview()
	CalibrationSaveDataset(req CalibrationSaveRequest) (*CalibrationSaveResult, error)

	CompositorOpenFilesDialog() ([]string, error)
	CompositorStitch(req CompositorStitchRequest) (*CompositorResult, error)
	CompositorAlignOverlay(req CompositorStitchRequest) (*CompositorResult, error)
	CompositorAddMask(req CompositorMaskRequest) (*CompositorResult, error)
	CompositorUndoMask() (*CompositorResult, error)
	CompositorClearMasks() (*CompositorResult, error)
	CompositorRefreshPreview() (*CompositorResult, error)
	CompositorLoadResult(req CompositorLoadResultRequest) (*ImageInfo, error)
}
