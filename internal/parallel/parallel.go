// Package parallel provides small bounded parallel loops for image processing.
package parallel

import "sync"

// For divides [0, total) into at most workers contiguous chunks and waits for
// every chunk to complete.
func For(total, workers int, fn func(start, end int)) {
	if workers <= 1 || total <= 1 {
		fn(0, total)
		return
	}
	if workers > total {
		workers = total
	}

	chunk := (total + workers - 1) / workers
	var wg sync.WaitGroup
	for worker := 0; worker < workers; worker++ {
		start := worker * chunk
		if start >= total {
			break
		}
		end := min(start+chunk, total)
		wg.Add(1)
		go func(start, end int) {
			defer wg.Done()
			fn(start, end)
		}(start, end)
	}
	wg.Wait()
}
