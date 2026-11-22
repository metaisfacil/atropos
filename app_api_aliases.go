package main

import (
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

func parseHexColor(s string) (color.NRGBA, error) {
	return appapi.ParseHexColor(s)
}

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
