package cli

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
)

func TestProcessFilesParallelPreservesOrder(t *testing.T) {
	files := []string{"a", "b", "c", "d", "e"}
	cfg := ProcessorConfig{MaxWorkers: 2}

	results := processFilesParallel(context.Background(), files, cfg,
		func(_ context.Context, file string, _ ProcessorConfig) FileResult {
			return FileResult{File: file}
		})

	if len(results) != len(files) {
		t.Fatalf("got %d results, want %d", len(results), len(files))
	}
	for i, want := range files {
		if results[i].File != want {
			t.Errorf("results[%d].File = %q, want %q", i, results[i].File, want)
		}
	}
}

func TestProcessFilesParallelRespectsMaxWorkers(t *testing.T) {
	const maxWorkers = 3
	var mu sync.Mutex
	var inFlight, peak int

	files := make([]string, 20)
	for i := range files {
		files[i] = "file"
	}

	processFilesParallel(context.Background(), files, ProcessorConfig{MaxWorkers: maxWorkers},
		func(_ context.Context, file string, _ ProcessorConfig) FileResult {
			mu.Lock()
			inFlight++
			if inFlight > peak {
				peak = inFlight
			}
			mu.Unlock()

			mu.Lock()
			inFlight--
			mu.Unlock()
			return FileResult{File: file}
		})

	if peak > maxWorkers {
		t.Errorf("peak concurrency %d exceeded MaxWorkers %d", peak, maxWorkers)
	}
}

func TestProcessFilesParallelCallsEveryFileOnce(t *testing.T) {
	files := []string{"a", "b", "c"}
	var calls atomic.Int64

	processFilesParallel(context.Background(), files, ProcessorConfig{MaxWorkers: 4},
		func(_ context.Context, file string, _ ProcessorConfig) FileResult {
			calls.Add(1)
			return FileResult{File: file}
		})

	if got := calls.Load(); got != int64(len(files)) {
		t.Errorf("processFn called %d times, want %d", got, len(files))
	}
}

func TestProcessFilesParallelEmptyInput(t *testing.T) {
	results := processFilesParallel(context.Background(), nil, ProcessorConfig{MaxWorkers: 1},
		func(_ context.Context, file string, _ ProcessorConfig) FileResult {
			t.Error("processFn should not be called for an empty file list")
			return FileResult{}
		})

	if len(results) != 0 {
		t.Errorf("got %d results, want 0", len(results))
	}
}

func TestProcessFilesParallelPassesConfig(t *testing.T) {
	cfg := ProcessorConfig{MaxWorkers: 1, Verbose: true, SkipBuild: true, SkipHealthcheck: true}

	results := processFilesParallel(context.Background(), []string{"a"}, cfg,
		func(_ context.Context, file string, got ProcessorConfig) FileResult {
			if got != cfg {
				t.Errorf("processFn received %+v, want %+v", got, cfg)
			}
			return FileResult{File: file, InstructionCnt: 7, DirectiveCnt: 2}
		})

	if results[0].InstructionCnt != 7 || results[0].DirectiveCnt != 2 {
		t.Errorf("result counts not propagated: %+v", results[0])
	}
}
