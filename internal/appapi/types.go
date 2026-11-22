package appapi

import (
	"fmt"
	"image"
	"image/color"
	"strings"

	"atropos/internal/compositor"
)

// FileFilter describes a native file dialog extension filter.
type FileFilter struct {
	DisplayName string
	Patterns    []string
}

// AppHost abstracts windowing, native file dialogs, URL opening, and async UI
// notifications away from the image processing backend.
type AppHost interface {
	SetWindowTitle(title string)
	Quit()
	OpenFileDialog(title, defaultDir, defaultName string, filters []FileFilter) (string, error)
	OpenMultipleFilesDialog(title, defaultDir string, filters []FileFilter) ([]string, error)
	SaveFileDialog(title, defaultDir, defaultName string, filters []FileFilter) (string, error)
	OpenURL(url string) error
	EmitTouchupDone(ev TouchUpDoneEvent)
}

// ProcessResult is the standard response for image processing operations,
// carrying an optional preview, status message, and image dimensions.
type ProcessResult struct {
	CropSkipped bool `json:"cropSkipped,omitempty"`
	// History-only fields synchronize controls without triggering a disc redraw.
	HistoryDiscSettings *DiscSettings `json:"historyDiscSettings,omitempty"`
	HistoryFeatherSize  int           `json:"historyFeatherSize,omitempty"`
	Preview             string        `json:"preview"`
	Message             string        `json:"message"`
	Width               int           `json:"width"`
	Height              int           `json:"height"`
	// Changed is set by operations that can legitimately produce no pixel
	// changes, allowing the frontend to preserve its unsaved-state indicator.
	Changed bool `json:"changed,omitempty"`
	// Optional numeric results (e.g. from AutoContrast)
	Black int `json:"black,omitempty"`
	White int `json:"white,omitempty"`
	// Corners is populated by DetectCorners and ResetCorners so the frontend
	// can render the overlay instead of baking dots into the image.
	Corners []image.Point `json:"corners,omitempty"`
	// SelectedCorners is populated by Undo when restoring a pre-warp corner
	// selection, so the frontend can restore the in-progress corner dots.
	SelectedCorners []image.Point `json:"selectedCorners,omitempty"`
	// Uncropped is set by Undo when the popped entry was saved before any warp
	// had produced a warpedImage — i.e. undoing the initial crop itself. The
	// frontend uses this to return the UI to the cropping phase.
	Uncropped bool `json:"uncropped,omitempty"`
	// DescreenReset is true when the operation invalidated an active descreen
	// session, meaning the next Descreen call will re-snapshot from scratch.
	// The frontend uses this to untoggle the Descreen button as a subtle hint.
	DescreenReset bool `json:"descreenReset,omitempty"`
	// UnmaskedPreview is the preview of disc source image without the disc mask
	// above it, used during live preview drag operations.
	UnmaskedPreview string `json:"unmaskedPreview,omitempty"`
	// Disc preview metadata (returned for mode sync)
	DiscCenterX  int     `json:"discCenterX,omitempty"`
	DiscCenterY  int     `json:"discCenterY,omitempty"`
	DiscRadius   int     `json:"discRadius,omitempty"`
	DiscRotation float64 `json:"discRotation,omitempty"`
	DiscBgR      int     `json:"discBgR,omitempty"`
	DiscBgG      int     `json:"discBgG,omitempty"`
	DiscBgB      int     `json:"discBgB,omitempty"`
}

// LaunchArgs contains the initial file path and mode from CLI arguments.
type LaunchArgs struct {
	FilePath        string `json:"filePath"`
	Mode            string `json:"mode"`
	Debug           bool   `json:"debug,omitempty"`
	PostSaveCommand string `json:"postSaveCommand,omitempty"`
	PostSaveEnabled bool   `json:"postSaveEnabled,omitempty"`
	PostSaveExit    bool   `json:"postSaveExit,omitempty"`
}

// LoadImageRequest contains the file path for loading an image.
type LoadImageRequest struct {
	FilePath string `json:"filePath"`
}

// LoadImageBytesRequest contains image bytes for loading external image data.
type LoadImageBytesRequest struct {
	Data []byte `json:"data"`
	Name string `json:"name,omitempty"`
}

// SuggestedCornerParams holds auto-computed corner detection defaults derived
// from objective image properties (dimensions, luminance statistics).
type SuggestedCornerParams struct {
	MinDistance int `json:"minDistance"`
	MaxCorners  int `json:"maxCorners"`
}

// ImageInfo contains image metadata and preview data.
type ImageInfo struct {
	Width   int     `json:"width"`
	Height  int     `json:"height"`
	Preview string  `json:"preview"`
	Format  string  `json:"format"` // e.g. "JPEG", "PNG", "TIFF", "BMP"
	DPIX    float64 `json:"dpiX"`   // horizontal DPI; 0 if unknown
	DPIY    float64 `json:"dpiY"`   // vertical DPI; 0 if unknown

	SuggestedCornerParams SuggestedCornerParams `json:"suggestedCornerParams"`
}

// SaveRequest specifies the output file path for saving the processed image.
type SaveRequest struct {
	OutputPath string `json:"outputPath"`
}

// AdjustmentSelection is an optional image-space rectangle used to limit a
// pixel adjustment. Coordinates may arrive in either drag direction.
type AdjustmentSelection struct {
	X1 float64 `json:"x1"`
	Y1 float64 `json:"y1"`
	X2 float64 `json:"x2"`
	Y2 float64 `json:"y2"`
}

// CropRequest specifies which edge to crop and how many source pixels to remove.
type CropRequest struct {
	Direction string `json:"direction"`
	Amount    int    `json:"amount"`
}

// RotateRequest specifies the rotation/flip operation to apply.
type RotateRequest struct {
	FlipCode int `json:"flipCode"`
}

// ResizeRequest specifies the target width and height for resizing an image.
type ResizeRequest struct {
	Width  int `json:"width"`
	Height int `json:"height"`
}

// SetLevelsRequest carries explicit black- and white-point values.
type SetLevelsRequest struct {
	Black     int                  `json:"black"`
	White     int                  `json:"white"`
	Selection *AdjustmentSelection `json:"selection,omitempty"`
}

// AutoContrastRequest optionally limits auto contrast to an image-space selection.
type AutoContrastRequest struct {
	Selection *AdjustmentSelection `json:"selection,omitempty"`
}

// DescreenRequest carries parameters for the FFT-based descreen filter.
type DescreenRequest struct {
	Thresh    int                  `json:"thresh"`
	Radius    int                  `json:"radius"`
	Middle    int                  `json:"middle"`
	Highlight int                  `json:"highlight"`
	Fast      bool                 `json:"fast"`
	Selection *AdjustmentSelection `json:"selection,omitempty"`
}

// CornerDetectRequest contains parameters for the Shi-Tomasi corner detector.
type CornerDetectRequest struct {
	MaxCorners   int     `json:"maxCorners"`
	QualityLevel float64 `json:"qualityLevel"`
	MinDistance  int     `json:"minDistance"`
	AccentValue  int     `json:"accentValue"`
	UseStretch   bool    `json:"useStretch"`
	StretchLow   float64 `json:"stretchLow"`
	StretchHigh  float64 `json:"stretchHigh"`
}

// ClickCornerRequest holds the image-space coordinates of a user click.
type ClickCornerRequest struct {
	X      int  `json:"x"`
	Y      int  `json:"y"`
	Custom bool `json:"custom"`
}

// ClickCornerResult is returned after each corner click.
// For clicks 1–3 only SnappedX/SnappedY/Count/Message are set; Preview is empty.
// On the 4th click Done is true, Preview contains the warped image, and
// Width/Height reflect the new image dimensions.
type ClickCornerResult struct {
	Preview       string `json:"preview"`
	Message       string `json:"message"`
	Count         int    `json:"count"`
	Done          bool   `json:"done"`
	SnappedX      int    `json:"snappedX"`
	SnappedY      int    `json:"snappedY"`
	Width         int    `json:"width"`
	Height        int    `json:"height"`
	DescreenReset bool   `json:"descreenReset,omitempty"`
}

// RestoreCornerOverlayRequest is the argument for RestoreCornerOverlay.
type RestoreCornerOverlayRequest struct {
	DotRadius int `json:"dotRadius"`
}

// DiscDrawRequest specifies the centre point and radius for a circular disc crop.
type DiscDrawRequest struct {
	CenterX int `json:"centerX"`
	CenterY int `json:"centerY"`
	Radius  int `json:"radius"`
}

// DiscRotateRequest specifies the rotation angle for disc mode.
type DiscRotateRequest struct {
	Angle float64 `json:"angle"`
}

// PixelColorRequest holds image-space coordinates for colour sampling.
type PixelColorRequest struct {
	X int `json:"x"`
	Y int `json:"y"`
}

// ShiftDiscRequest holds the pixel offset to shift the disc centre by.
type ShiftDiscRequest struct {
	DX int `json:"dx"`
	DY int `json:"dy"`
}

// FeatherSizeRequest holds the new feather width.
type FeatherSizeRequest struct {
	Size int `json:"size"`
}

// FeatherRadiusRequest holds the new outer radius for an active disc crop.
type FeatherRadiusRequest struct {
	Radius int `json:"radius"`
}

// StraightEdgeRotateRequest carries the angle (in degrees) of the reference
// line drawn by the user in display space. The backend subtracts it from
// rotationAngle so the edge becomes perfectly horizontal.
type StraightEdgeRotateRequest struct {
	AngleDeg float64 `json:"angleDeg"`
}

// DustRemovalRequest selects one of three dust-removal strength banks.
// DPI controls the detector's morphology and area scaling; a non-positive
// value falls back to 300 DPI.
type DustRemovalRequest struct {
	Level     string               `json:"level"`
	DPI       float64              `json:"dpi"`
	Selection *AdjustmentSelection `json:"selection,omitempty"`
}

// LineAddRequest defines start and end coordinates for a user-drawn line
// used in 4-line perspective correction mode.
type LineAddRequest struct {
	X1 int `json:"x1"`
	Y1 int `json:"y1"`
	X2 int `json:"x2"`
	Y2 int `json:"y2"`
}

// NormalCropRequest holds the image-space bounding box for a rectangle crop.
type NormalCropRequest struct {
	X1 int `json:"x1"`
	Y1 int `json:"y1"`
	X2 int `json:"x2"`
	Y2 int `json:"y2"`
}

// TouchUpPoint is an image-space point in a touch-up brush stroke.
type TouchUpPoint struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}

// TouchUpStrokeRequest is the compact brush representation sent by the UI.
type TouchUpStrokeRequest struct {
	Points     []TouchUpPoint       `json:"points"`
	BrushSize  float64              `json:"brushSize"`
	PatchSize  int                  `json:"patchSize"`
	Iterations int                  `json:"iterations"`
	Selection  *AdjustmentSelection `json:"selection,omitempty"`
}

// TouchUpDoneEvent is the payload emitted when async touch-up completes or cancels.
type TouchUpDoneEvent struct {
	Cancelled     bool                 `json:"cancelled,omitempty"`
	Error         string               `json:"error,omitempty"`
	Preview       string               `json:"preview,omitempty"`
	Patch         *TouchUpPreviewPatch `json:"patch,omitempty"`
	Message       string               `json:"message,omitempty"`
	Width         int                  `json:"width,omitempty"`
	Height        int                  `json:"height,omitempty"`
	DescreenReset bool                 `json:"descreenReset,omitempty"`
}

// TouchUpPreviewPatch carries a PNG patch covering the modified brush region.
type TouchUpPreviewPatch struct {
	DataURL string `json:"dataURL"`
	X       int    `json:"x"`
	Y       int    `json:"y"`
	Width   int    `json:"width"`
	Height  int    `json:"height"`
}

// AllSettings is the complete set of user-facing persistent settings.
type AllSettings struct {
	// Touch-up
	TouchupBackend string `json:"touchupBackend"`
	IOPaintURL     string `json:"iopaintUrl"`
	// Warp fill
	WarpFillMode  string `json:"warpFillMode"`
	WarpFillColor string `json:"warpFillColor"`
	// Keyboard edge crop
	CropEdgePixels int `json:"cropEdgePixels"`
	// Disc
	DiscCenterCutout  bool `json:"discCenterCutout"`
	DiscCutoutPercent int  `json:"discCutoutPercent"`
	// Corner detection
	CornerMaxCorners      int `json:"cornerMaxCorners"`
	CornerMinDistance     int `json:"cornerMinDistance"`
	CornerSettingsVersion int `json:"cornerSettingsVersion"`
	// Behaviour flags
	AutoCornerParams          bool   `json:"autoCornerParams"`
	CloseAfterSave            bool   `json:"closeAfterSave"`
	PostSaveEnabled           bool   `json:"postSaveEnabled"`
	PostSaveCommand           string `json:"postSaveCommand"`
	TouchupRemainsActive      bool   `json:"touchupRemainsActive"`
	StraightEdgeRemainsActive bool   `json:"straightEdgeRemainsActive"`
	AutoDetectOnModeSwitch    bool   `json:"autoDetectOnModeSwitch"`
	AppVersion                string `json:"appVersion"`
	Initialized               bool   `json:"-"`
}

// TouchupSettings holds the configuration for the touch-up backend.
type TouchupSettings struct {
	Backend    string `json:"backend"`
	IOPaintURL string `json:"iopaintUrl"`
}

// WarpSettings holds configuration for how out-of-bounds regions produced by
// perspective warping are handled.
type WarpSettings struct {
	// FillMode is "clamp", "fill" (solid colour), or "outpaint" (PatchMatch).
	FillMode string `json:"fillMode"`
	// FillColor is a CSS hex colour string (e.g. "#ffffff") used when FillMode=="fill".
	FillColor string `json:"fillColor"`
}

// DiscSettings holds configuration for disc-mode rendering.
type DiscSettings struct {
	CenterCutout  bool `json:"centerCutout"`
	CutoutPercent int  `json:"cutoutPercent"`
}

// ParseHexColor parses a CSS hex colour string ("#rrggbb" or "#rgb") into color.NRGBA.
func ParseHexColor(s string) (color.NRGBA, error) {
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

// CalibrationLoadRequest specifies a calibration image file path to decode.
type CalibrationLoadRequest struct {
	FilePath string `json:"filePath"`
}

// CalibrationImageInfo describes a loaded calibration scan.
type CalibrationImageInfo struct {
	ImagePath string `json:"imagePath"`
	Preview   string `json:"preview"`
	Width     int    `json:"width"`
	Height    int    `json:"height"`
}

// CalibrationPoint represents an annotated corner coordinate in pixels.
type CalibrationPoint struct {
	X int `json:"x"`
	Y int `json:"y"`
}

// CalibrationEntryRequest holds one annotated calibration scan entry.
type CalibrationEntryRequest struct {
	ImagePath string             `json:"imagePath"`
	Width     int                `json:"width"`
	Height    int                `json:"height"`
	Corners   []CalibrationPoint `json:"corners"`
}

// CalibrationSaveRequest carries the completed entries for JSON export.
type CalibrationSaveRequest struct {
	Entries []CalibrationEntryRequest `json:"entries"`
}

// CalibrationSaveResult describes the outcome of exporting calibration data.
type CalibrationSaveResult struct {
	OutputPath string `json:"outputPath"`
	Count      int    `json:"count"`
	Cancelled  bool   `json:"cancelled,omitempty"`
}

// CompositorStitchRequest carries the ordered list of source file paths,
// optional per-image quarter-turn clockwise rotations (mod 4), and the
// orientation that describes how consecutive images are arranged.
type CompositorStitchRequest struct {
	ImagePaths    []string `json:"imagePaths"`
	Orientation   string   `json:"orientation"`
	RotationSteps []int    `json:"rotationSteps,omitempty"`
}

// CompositorResult is returned by CompositorStitch and overlay preview calls.
type CompositorResult struct {
	Preview string `json:"preview"`
	Width   int    `json:"width"`
	Height  int    `json:"height"`
	Message string `json:"message"`
}

// CompositorSaveRequest carries the output path for saving the stitched image.
type CompositorSaveRequest struct {
	OutputPath string `json:"outputPath"`
}

// CompositorMaskRequest carries a single overlay reveal mask stroke.
type CompositorMaskRequest struct {
	Stroke compositor.MaskStroke `json:"stroke"`
}

// CompositorLoadResultRequest carries optional post-processing for the
// compositor output before it enters the editing pipeline.
type CompositorLoadResultRequest struct {
	RotationSteps int `json:"rotationSteps"`
}

// PreviewViewportRequest asks the backend for only the source rectangle needed
// by the current viewport.
type PreviewViewportRequest struct {
	Preview    string `json:"preview"`
	X          int    `json:"x"`
	Y          int    `json:"y"`
	Width      int    `json:"width"`
	Height     int    `json:"height"`
	DestWidth  int    `json:"destWidth"`
	DestHeight int    `json:"destHeight"`
	Quality    int    `json:"quality"`
}

// PreviewViewportResponse carries a viewport JPEG as a data URL.
type PreviewViewportResponse struct {
	DataURL      string `json:"dataURL"`
	X            int    `json:"x"`
	Y            int    `json:"y"`
	Width        int    `json:"width"`
	Height       int    `json:"height"`
	RasterWidth  int    `json:"rasterWidth"`
	RasterHeight int    `json:"rasterHeight"`
}
