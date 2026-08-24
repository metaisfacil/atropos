package cornerdetect

import "image"

type cornerCandidate struct {
	pt  image.Point
	val float64
}

// selectSpacedCorners consumes candidates in descending response order without
// sorting the entire slice. A max-heap makes the work proportional to the
// candidates actually examined, while the spatial grid makes each minimum-
// distance check local instead of scanning every previously accepted corner.
func selectSpacedCorners(candidates []cornerCandidate, maxCorners, minDistance int) []image.Point {
	if maxCorners <= 0 || len(candidates) == 0 {
		return nil
	}
	cornerCandidateHeapify(candidates)

	cellSize := minDistance
	if cellSize < 1 {
		cellSize = 1
	}
	minDistSq := int64(minDistance) * int64(minDistance)
	type gridCell struct{ x, y int }
	// Each cell stores a one-based head index into result/next. This avoids one
	// small slice allocation per occupied cell while still permitting multiple
	// accepted corners in the same cell.
	grid := make(map[gridCell]int, maxCorners)
	result := make([]image.Point, 0, maxCorners)
	next := make([]int, 0, maxCorners)

	for len(candidates) > 0 && len(result) < maxCorners {
		var candidate cornerCandidate
		candidate, candidates = cornerCandidateHeapPop(candidates)
		cell := gridCell{x: candidate.pt.X / cellSize, y: candidate.pt.Y / cellSize}
		tooClose := false
		for gy := cell.y - 1; gy <= cell.y+1 && !tooClose; gy++ {
			for gx := cell.x - 1; gx <= cell.x+1 && !tooClose; gx++ {
				for link := grid[gridCell{x: gx, y: gy}]; link != 0; link = next[link-1] {
					accepted := result[link-1]
					dx := int64(candidate.pt.X - accepted.X)
					dy := int64(candidate.pt.Y - accepted.Y)
					if dx*dx+dy*dy < minDistSq {
						tooClose = true
						break
					}
				}
			}
		}
		if !tooClose {
			result = append(result, candidate.pt)
			next = append(next, grid[cell])
			grid[cell] = len(result)
		}
	}
	return result
}

func cornerCandidateHeapify(values []cornerCandidate) {
	for root := len(values)/2 - 1; root >= 0; root-- {
		cornerCandidateHeapDown(values, root)
	}
}

func cornerCandidateHeapPop(values []cornerCandidate) (cornerCandidate, []cornerCandidate) {
	result := values[0]
	last := len(values) - 1
	values[0] = values[last]
	values = values[:last]
	if len(values) > 0 {
		cornerCandidateHeapDown(values, 0)
	}
	return result, values
}

func cornerCandidateHeapDown(values []cornerCandidate, root int) {
	for {
		left := root*2 + 1
		if left >= len(values) {
			return
		}
		largest := left
		right := left + 1
		if right < len(values) && values[right].val > values[left].val {
			largest = right
		}
		if values[root].val >= values[largest].val {
			return
		}
		values[root], values[largest] = values[largest], values[root]
		root = largest
	}
}
