package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"net/http"
	"strings"
	"time"
)

// iopaintRequest mirrors the JSON body expected by the IOPaint /api/v1/inpaint endpoint.
type iopaintRequest struct {
	Image                       string      `json:"image"`
	Mask                        string      `json:"mask"`
	LDMSteps                    int         `json:"ldm_steps"`
	LDMSampler                  string      `json:"ldm_sampler"`
	ZITSWireframe               bool        `json:"zits_wireframe"`
	CV2Flag                     string      `json:"cv2_flag"`
	CV2Radius                   int         `json:"cv2_radius"`
	HDStrategy                  string      `json:"hd_strategy"`
	HDStrategyCropTriggerSize   int         `json:"hd_strategy_crop_triger_size"`
	HDStrategyCropMargin        int         `json:"hd_strategy_crop_margin"`
	HDStrategyResizeLimit       int         `json:"hd_trategy_resize_imit"`
	Prompt                      string      `json:"prompt"`
	NegativePrompt              string      `json:"negative_prompt"`
	UseCroper                   bool        `json:"use_croper"`
	CroperX                     int         `json:"croper_x"`
	CroperY                     int         `json:"croper_y"`
	CroperHeight                int         `json:"croper_height"`
	CroperWidth                 int         `json:"croper_width"`
	UseExtender                 bool        `json:"use_extender"`
	ExtenderX                   int         `json:"extender_x"`
	ExtenderY                   int         `json:"extender_y"`
	ExtenderHeight              int         `json:"extender_height"`
	ExtenderWidth               int         `json:"extender_width"`
	SDMaskBlur                  int         `json:"sd_mask_blur"`
	SDStrength                  float64     `json:"sd_strength"`
	SDSteps                     int         `json:"sd_steps"`
	SDGuidanceScale             float64     `json:"sd_guidance_scale"`
	SDSampler                   string      `json:"sd_sampler"`
	SDSeed                      int         `json:"sd_seed"`
	SDMatchHistograms           bool        `json:"sd_match_histograms"`
	SDLCMLora                   bool        `json:"sd_lcm_lora"`
	PaintByExampleExampleImage  interface{} `json:"paint_by_example_example_image"`
	P2PImageGuidanceScale       float64     `json:"p2p_image_guidance_scale"`
	EnableControlnet            bool        `json:"enable_controlnet"`
	ControlnetConditioningScale float64     `json:"controlnet_conditioning_scale"`
	ControlnetMethod            string      `json:"controlnet_method"`
	EnableBrushnet              bool        `json:"enable_brushnet"`
	BrushnetMethod              string      `json:"brushnet_method"`
	BrushnetConditioningScale   float64     `json:"brushnet_conditioning_scale"`
	EnablePowerpaintV2          bool        `json:"enable_powerpaint_v2"`
	PowerpaintTask              string      `json:"powerpaint_task"`
}

// iopaintFill calls the IOPaint /api/v1/inpaint endpoint and returns the full
// result image with the masked region inpainted.
// mask: alpha > 0 marks pixels to be filled (white in the mask sent to IOPaint).
// ctx cancels the in-flight HTTP request if the caller is aborted.
func (a *App) iopaintFill(ctx context.Context, src *image.NRGBA, mask *image.Alpha) (*image.NRGBA, error) {
	// Encode source image as PNG, then base64.
	var imgBuf bytes.Buffer
	pngEnc := png.Encoder{CompressionLevel: png.BestSpeed}
	if err := pngEnc.Encode(&imgBuf, src); err != nil {
		return nil, fmt.Errorf("iopaint: encode source image: %w", err)
	}
	imgB64 := "data:image/png;base64," + base64.StdEncoding.EncodeToString(imgBuf.Bytes())

	// Build a grayscale mask PNG: white (255) where masked, black (0) elsewhere.
	b := src.Bounds()
	maskGray := image.NewGray(b)
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			if mask.AlphaAt(x, y).A > 0 {
				maskGray.SetGray(x, y, color.Gray{Y: 255})
			}
		}
	}
	var maskBuf bytes.Buffer
	if err := pngEnc.Encode(&maskBuf, maskGray); err != nil {
		return nil, fmt.Errorf("iopaint: encode mask: %w", err)
	}
	maskB64 := "data:image/png;base64," + base64.StdEncoding.EncodeToString(maskBuf.Bytes())

	reqBody := iopaintRequest{
		Image:                       imgB64,
		Mask:                        maskB64,
		LDMSteps:                    30,
		LDMSampler:                  "ddim",
		ZITSWireframe:               true,
		CV2Flag:                     "INPAINT_NS",
		CV2Radius:                   5,
		HDStrategy:                  "Crop",
		HDStrategyCropTriggerSize:   640,
		HDStrategyCropMargin:        128,
		HDStrategyResizeLimit:       2048,
		Prompt:                      "",
		NegativePrompt:              "",
		UseCroper:                   false,
		CroperX:                     0,
		CroperY:                     0,
		CroperHeight:                512,
		CroperWidth:                 512,
		UseExtender:                 false,
		ExtenderX:                   0,
		ExtenderY:                   0,
		ExtenderHeight:              512,
		ExtenderWidth:               512,
		SDMaskBlur:                  12,
		SDStrength:                  1,
		SDSteps:                     50,
		SDGuidanceScale:             7.5,
		SDSampler:                   "DPM++ 2M",
		SDSeed:                      -1,
		SDMatchHistograms:           false,
		SDLCMLora:                   false,
		PaintByExampleExampleImage:  nil,
		P2PImageGuidanceScale:       1.5,
		EnableControlnet:            false,
		ControlnetConditioningScale: 0.4,
		ControlnetMethod:            "",
		EnableBrushnet:              false,
		BrushnetMethod:              "random_mask",
		BrushnetConditioningScale:   1,
		EnablePowerpaintV2:          false,
		PowerpaintTask:              "text-guided",
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("iopaint: marshal request: %w", err)
	}

	endpoint := strings.TrimRight(a.iopaintURL, "/") + "/api/v1/inpaint"
	a.logf("iopaintFill: POST %s (body=%d bytes)", endpoint, len(bodyBytes))

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("iopaint: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: 120 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("iopaint: POST %s: %w", endpoint, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("iopaint: server returned %d: %s", resp.StatusCode, string(body))
	}

	respBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("iopaint: read response: %w", err)
	}
	a.logf("iopaintFill: response %d bytes", len(respBytes))

	// Try raw image bytes first (IOPaint typically returns PNG directly).
	out, _, decErr := image.Decode(bytes.NewReader(respBytes))
	if decErr != nil {
		// Fall back: try JSON {"image": "data:...;base64,..."}.
		var jsonResp struct {
			Image string `json:"image"`
		}
		if jsonErr := json.Unmarshal(respBytes, &jsonResp); jsonErr == nil && jsonResp.Image != "" {
			imgData := jsonResp.Image
			if idx := strings.Index(imgData, ","); idx >= 0 {
				imgData = imgData[idx+1:]
			}
			if decoded, b64Err := base64.StdEncoding.DecodeString(imgData); b64Err == nil {
				out, _, decErr = image.Decode(bytes.NewReader(decoded))
			}
		}
		if decErr != nil {
			return nil, fmt.Errorf("iopaint: decode response image: %w", decErr)
		}
	}

	return toNRGBA(out), nil
}
