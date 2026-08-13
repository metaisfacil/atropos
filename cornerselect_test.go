package main

import (
	"image"
	"reflect"
	"sort"
	"testing"
)

func TestSelectSpacedCornersMatchesSortedReference(t *testing.T) {
	for _, minDistance := range []int{1, 2, 7, 25} {
		candidates := make([]cornerCandidate, 2000)
		for i := range candidates {
			// Responses are unique, making the descending order independent of
			// sort stability while still distributing points densely.
			candidates[i] = cornerCandidate{
				pt:  image.Pt((i*73)%311, (i*151)%233),
				val: float64((i*15485863)%32452843) + float64(i)/10000,
			}
		}
		wantInput := append([]cornerCandidate(nil), candidates...)
		gotInput := append([]cornerCandidate(nil), candidates...)
		want := selectSpacedCornersReference(wantInput, 150, minDistance)
		got := selectSpacedCorners(gotInput, 150, minDistance)
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("minDistance=%d: selection differs\ngot:  %v\nwant: %v", minDistance, got, want)
		}
	}
}

func TestSelectSpacedCornersEmpty(t *testing.T) {
	if got := selectSpacedCorners(nil, 100, 5); got != nil {
		t.Fatalf("expected nil result, got %v", got)
	}
}

func selectSpacedCornersReference(candidates []cornerCandidate, maxCorners, minDistance int) []image.Point {
	sort.Slice(candidates, func(i, j int) bool { return candidates[i].val > candidates[j].val })
	minDistSq := int64(minDistance) * int64(minDistance)
	var result []image.Point
	for _, candidate := range candidates {
		if len(result) >= maxCorners {
			break
		}
		tooClose := false
		for _, accepted := range result {
			dx := int64(candidate.pt.X - accepted.X)
			dy := int64(candidate.pt.Y - accepted.Y)
			if dx*dx+dy*dy < minDistSq {
				tooClose = true
				break
			}
		}
		if !tooClose {
			result = append(result, candidate.pt)
		}
	}
	return result
}
