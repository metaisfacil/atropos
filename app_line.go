package main

import (
	"fmt"
	"image"
	"math"
)

// LineAddRequest defines start and end coordinates for a user-drawn line
// used in 4-line perspective correction mode.
type LineAddRequest struct {
	X1 int `json:"x1"`
	Y1 int `json:"y1"`
	X2 int `json:"x2"`
	Y2 int `json:"y2"`
}

// AddLine records a line for line-based perspective correction.
func (a *App) AddLine(req LineAddRequest) (*ProcessResult, error) {
	a.logf("AddLine: (%d,%d)-(%d,%d)", req.X1, req.Y1, req.X2, req.Y2)
	a.lines = append(a.lines, []image.Point{
		{X: req.X1, Y: req.Y1},
		{X: req.X2, Y: req.Y2},
	})
	return &ProcessResult{
		Message: fmt.Sprintf("Lines: %d/4", len(a.lines)),
	}, nil
}

// ProcessLines calculates corner intersections and applies perspective correction.
func (a *App) ProcessLines() (*ProcessResult, error) {
	a.logf("ProcessLines: lines=%d", len(a.lines))
	if len(a.lines) != 4 {
		return nil, fmt.Errorf("need exactly 4 lines")
	}

	var intersections []image.Point
	for i := 0; i < 4; i++ {
		for j := i + 1; j < 4; j++ {
			if pt := lineIntersection(a.lines[i], a.lines[j]); pt != nil {
				intersections = append(intersections, *pt)
			}
		}
	}

	if len(intersections) < 4 {
		return nil, fmt.Errorf("could not find 4 corner intersections")
	}

	// Filter out intersections too far outside image bounds
	ob := a.originalImage.Bounds()
	marginX := float64(ob.Dx()) * 0.5
	marginY := float64(ob.Dy()) * 0.5
	var valid []image.Point
	for _, p := range intersections {
		px, py := float64(p.X), float64(p.Y)
		if px >= -marginX && px <= float64(ob.Max.X)+marginX &&
			py >= -marginY && py <= float64(ob.Max.Y)+marginY {
			valid = append(valid, p)
		}
	}

	if len(valid) < 4 {
		return nil, fmt.Errorf("not enough valid intersections (%d found)", len(valid))
	}

	// If more than 4, pick the 4 farthest from centroid
	if len(valid) > 4 {
		cx, cy := 0.0, 0.0
		for _, p := range valid {
			cx += float64(p.X)
			cy += float64(p.Y)
		}
		cx /= float64(len(valid))
		cy /= float64(len(valid))

		type scoredPt struct {
			pt   image.Point
			dist float64
		}
		scored := make([]scoredPt, len(valid))
		for i, p := range valid {
			dx := float64(p.X) - cx
			dy := float64(p.Y) - cy
			scored[i] = scoredPt{p, dx*dx + dy*dy}
		}
		// Sort descending by distance
		for i := 0; i < len(scored)-1; i++ {
			for j := i + 1; j < len(scored); j++ {
				if scored[j].dist > scored[i].dist {
					scored[i], scored[j] = scored[j], scored[i]
				}
			}
		}
		valid = []image.Point{scored[0].pt, scored[1].pt, scored[2].pt, scored[3].pt}
	}

	sorted := orderPoints(valid[:4])
	tl, tr, br, bl := sorted[0], sorted[1], sorted[2], sorted[3]

	// Compute output dimensions from the quadrilateral edge lengths
	widthTop := dist(tl, tr)
	widthBot := dist(bl, br)
	heightLeft := dist(tl, bl)
	heightRight := dist(tr, br)
	outW := int(math.Max(widthTop, widthBot))
	outH := int(math.Max(heightLeft, heightRight))
	if outW < 10 {
		outW = 10
	}
	if outH < 10 {
		outH = 10
	}
	a.logf("ProcessLines: output %dx%d", outW, outH)

	dst := [4]image.Point{
		{0, 0}, {outW, 0}, {outW, outH}, {0, outH},
	}
	src := [4]image.Point{tl, tr, br, bl}

	warped := perspectiveTransform(a.originalImage, src, dst, outW, outH)
	a.warpedImage = warped
	a.lines = nil

	preview, err := imageToBase64(a.warpedImage)
	if err != nil {
		return nil, err
	}
	return &ProcessResult{Preview: preview}, nil
}

// ClearLines removes all drawn lines and restores the pre-line image.
func (a *App) ClearLines() (*ProcessResult, error) {
	a.logf("ClearLines")
	a.lines = nil
	a.warpedImage = nil

	if a.currentImage == nil {
		return &ProcessResult{Message: "Lines cleared"}, nil
	}
	preview, err := imageToBase64(a.currentImage)
	if err != nil {
		return nil, err
	}
	b := a.currentImage.Bounds()
	return &ProcessResult{
		Preview: preview,
		Message: "Lines cleared — draw 4 new lines",
		Width:   b.Dx(),
		Height:  b.Dy(),
	}, nil
}
