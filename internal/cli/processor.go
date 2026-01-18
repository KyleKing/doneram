package cli

import (
	"context"
	"sync"

	"github.com/kyleking/doner/internal/updater"
)

// FileResult contains the processing result for a single file.
type FileResult struct {
	File           string
	Updates        []updater.Update
	BuildSuccess   bool
	BuildError     error
	ProcessingErr  error // file read, parse errors
	InstructionCnt int
	DirectiveCnt   int
}

// ProcessorConfig contains configuration for file processing.
type ProcessorConfig struct {
	MaxWorkers      int
	Verbose         bool
	SkipBuild       bool // for update command
	SkipHealthcheck bool // for update command
}

// processFilesParallel processes multiple files in parallel using a worker pool.
// Results are returned in the same order as the input files.
func processFilesParallel(
	ctx context.Context,
	files []string,
	cfg ProcessorConfig,
	processFn func(context.Context, string, ProcessorConfig) FileResult,
) []FileResult {
	results := make([]FileResult, len(files))

	// Create semaphore for worker pool
	sem := make(chan struct{}, cfg.MaxWorkers)
	var wg sync.WaitGroup

	for i, file := range files {
		wg.Add(1)
		go func(idx int, f string) {
			defer wg.Done()

			// Acquire semaphore
			sem <- struct{}{}
			defer func() { <-sem }()

			// Process file
			results[idx] = processFn(ctx, f, cfg)
		}(i, file)
	}

	wg.Wait()
	return results
}
