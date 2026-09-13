package patchmatch

import (
	"context"
	"errors"
	"runtime"
	"sync/atomic"
	"testing"
)

func TestParallelRowsLeavesProcessorAvailable(t *testing.T) {
	previous := runtime.GOMAXPROCS(4)
	defer runtime.GOMAXPROCS(previous)
	var active, peak atomic.Int32
	visits := make([]atomic.Int32, 64)
	err := parallelRowsSized(context.Background(), 0, len(visits), 1024, func(y int) {
		n := active.Add(1)
		for old := peak.Load(); n > old; old = peak.Load() {
			if peak.CompareAndSwap(old, n) {
				break
			}
		}
		visits[y].Add(1)
		runtime.Gosched()
		active.Add(-1)
	})
	if err != nil {
		t.Fatal(err)
	}
	if peak.Load() > 3 {
		t.Fatalf("used %d concurrent workers; want at most 3", peak.Load())
	}
	for y := range visits {
		if n := visits[y].Load(); n != 1 {
			t.Fatalf("row %d processed %d times", y, n)
		}
	}
	if runtime.GOMAXPROCS(0) != 4 {
		t.Fatal("solver changed process-wide GOMAXPROCS")
	}
}

func TestParallelRowsSingleProcessorCancellation(t *testing.T) {
	previous := runtime.GOMAXPROCS(1)
	defer runtime.GOMAXPROCS(previous)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	started := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		<-started
		cancel()
	}()
	rows := 0
	err := parallelRowsSized(ctx, 0, 100, 1024, func(y int) {
		rows++
		if y == 0 {
			close(started)
		}
	})
	<-done
	if !errors.Is(err, context.Canceled) || rows >= 100 {
		t.Fatalf("cancellation did not interrupt rows: rows=%d err=%v", rows, err)
	}
}
