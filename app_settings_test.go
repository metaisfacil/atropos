package main

import "testing"

func TestSanitizeSettingsCornerParameters(t *testing.T) {
	tests := []struct {
		name            string
		maxCorners      int
		minDistance     int
		wantMaxCorners  int
		wantMinDistance int
	}{
		{name: "valid", maxCorners: 275, minDistance: 37, wantMaxCorners: 275, wantMinDistance: 37},
		{name: "zero", maxCorners: 0, minDistance: 0, wantMaxCorners: 500, wantMinDistance: 100},
		{name: "above range", maxCorners: 1001, minDistance: 201, wantMaxCorners: 500, wantMinDistance: 100},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := sanitizeSettings(AllSettings{
				TouchupBackend:        "patchmatch",
				IOPaintURL:            "http://127.0.0.1:8086/",
				WarpFillMode:          "clamp",
				WarpFillColor:         "#ffffff",
				DiscCutoutPercent:     11,
				CornerMaxCorners:      tt.maxCorners,
				CornerMinDistance:     tt.minDistance,
				CornerSettingsVersion: 1,
			})
			if got.CornerMaxCorners != tt.wantMaxCorners {
				t.Errorf("CornerMaxCorners = %d, want %d", got.CornerMaxCorners, tt.wantMaxCorners)
			}
			if got.CornerMinDistance != tt.wantMinDistance {
				t.Errorf("CornerMinDistance = %d, want %d", got.CornerMinDistance, tt.wantMinDistance)
			}
		})
	}
}
