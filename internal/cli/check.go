package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"regexp"
	"strings"

	"github.com/urfave/cli/v3"

	"github.com/kyleking/doner/internal/httpclient"
	"github.com/kyleking/doner/internal/parser"
	"github.com/kyleking/doner/internal/reporter"
	"github.com/kyleking/doner/internal/resolver"
	"github.com/kyleking/doner/internal/updater"
)

func newCheckCommand() *cli.Command {
	return &cli.Command{
		Name:  "check",
		Usage: "Check for available updates (dry-run)",
		Flags: []cli.Flag{
			&cli.StringSliceFlag{
				Name:    "file",
				Aliases: []string{"f"},
				Usage:   "path(s) to Dockerfile - supports globs (e.g., docker/*/Dockerfile, **/Dockerfile)",
			},
			&cli.StringFlag{
				Name:  "format",
				Usage: "output format: stdout, github-actions, json",
				Value: "stdout",
			},
			&cli.IntFlag{
				Name:  "workers",
				Usage: "number of parallel workers",
				Value: 4,
			},
		},
		Action: runCheck,
	}
}

func runCheck(ctx context.Context, cmd *cli.Command) error {
	patterns := cmd.StringSlice("file")
	verbose := cmd.Bool("verbose")
	format := cmd.String("format")
	workers := cmd.Int("workers")

	// Expand file patterns
	files, err := expandFiles(patterns)
	if err != nil {
		return err
	}

	rep := reporter.NewOutputReporter(format, verbose)

	if verbose {
		fmt.Printf("Checking %d file(s) for updates...\n", len(files))
	}

	// Process files in parallel
	cfg := ProcessorConfig{
		MaxWorkers: int(workers),
		Verbose:    verbose,
	}

	results := processFilesParallel(ctx, files, cfg, processCheckFile)

	// Report results sequentially
	var totalUpdates, successCnt, failedCnt int
	for _, result := range results {
		if result.ProcessingErr != nil {
			failedCnt++
			if verbose || len(files) > 1 {
				fmt.Printf("Error processing %s: %v\n", result.File, result.ProcessingErr)
			}
			continue
		}

		successCnt++
		totalUpdates += len(result.Updates)
		rep.ReportCheck(result.File, result.InstructionCnt, result.DirectiveCnt, result.Updates)
	}

	// Report summary for multi-file runs
	if len(files) > 1 {
		rep.ReportSummary(len(files), totalUpdates, successCnt, failedCnt)
	}

	if failedCnt > 0 {
		return fmt.Errorf("%d of %d file(s) failed processing", failedCnt, len(files))
	}

	return nil
}

// processCheckFile processes a single Dockerfile and returns the result.
func processCheckFile(ctx context.Context, file string, cfg ProcessorConfig) FileResult {
	logger := httpclient.LoggerFromContext(ctx)
	result := FileResult{
		File: file,
	}

	logger.Info("checking file", "file", file, "format", "dockerfile")

	content, err := os.ReadFile(file)
	if err != nil {
		result.ProcessingErr = fmt.Errorf("reading dockerfile: %w", err)
		return result
	}

	df, err := parser.Parse(string(content))
	if err != nil {
		result.ProcessingErr = fmt.Errorf("parsing dockerfile: %w", err)
		return result
	}

	result.InstructionCnt = len(df.Instructions)
	result.DirectiveCnt = len(df.Directives)

	httpClient := httpclient.New(httpclient.DefaultConfig())
	dockerHub := resolver.NewDockerHubResolver(httpClient)
	ghcr := resolver.NewGHCRResolver(httpClient)

	// Package manager resolvers (for future RUN instruction support)
	_ = resolver.NewPyPIResolver(httpClient)
	_ = resolver.NewNPMResolver(httpClient)
	_ = resolver.NewAPKResolver(httpClient)
	_ = resolver.NewAPTResolver(httpClient)
	_ = resolver.NewCargoResolver(httpClient)
	_ = resolver.NewRubyGemsResolver(httpClient)
	_ = resolver.NewComposerResolver(httpClient)
	_ = resolver.NewYumResolver(httpClient)

	directiveMap := make(map[int]*parser.Directive)
	for _, d := range df.Directives {
		directiveMap[d.Line] = d
	}

	// Resolve latest versions
	resolved := make(map[int]string)
	for _, instr := range df.Instructions {
		directive := directiveMap[instr.Line-1]
		if directive == nil || directive.Ignore {
			continue
		}

		var latest string
		var err error
		if instr.Command == "FROM" {
			latest, err = checkFromInstruction(ctx, instr, directive, dockerHub, ghcr)
		} else if instr.Command == "COPY" && strings.Contains(instr.Args, "--from=") {
			latest, err = checkCopyFromInstruction(ctx, instr, directive, dockerHub, ghcr)
		}

		if err != nil {
			var notFoundErr *httpclient.NotFoundError
			var rateLimitErr *httpclient.RateLimitError

			if errors.As(err, &notFoundErr) {
				logger.Debug("resource not found", "error", err)
			} else if errors.As(err, &rateLimitErr) {
				logger.Warn("rate limit encountered", "error", err)
			} else {
				logger.Error("resolution failed", "error", err)
			}
			continue
		}

		if latest != "" {
			resolved[instr.Line] = latest
		}
	}

	// Create updates list
	result.Updates = updater.UpdateFromInstructions(df.Instructions, directiveMap, resolved)

	logger.Info("check completed", "file", file, "updates_found", len(result.Updates))

	return result
}

var imageRegex = regexp.MustCompile(`^([^:]+):(.+)$`)

func checkFromInstruction(ctx context.Context, instr parser.Instruction, directive *parser.Directive, dockerHub, ghcr resolver.Resolver) (string, error) {
	image := strings.TrimSpace(instr.Args)

	matches := imageRegex.FindStringSubmatch(image)
	if matches == nil {
		return "", fmt.Errorf("invalid FROM format: %s", image)
	}

	imageName := matches[1]

	if len(directive.Packages) == 0 {
		return "", fmt.Errorf("no package directive found")
	}

	pkg := directive.Packages[0]
	if pkg.Ignore {
		return "", nil
	}

	var r resolver.Resolver
	if strings.Contains(imageName, "ghcr.io") {
		r = ghcr
	} else {
		r = dockerHub
	}

	latest, err := r.Resolve(ctx, imageName, pkg.Pattern)
	if err != nil {
		return "", err
	}

	return latest, nil
}

var copyFromRegex = regexp.MustCompile(`--from=([^\s]+)`)

func checkCopyFromInstruction(ctx context.Context, instr parser.Instruction, directive *parser.Directive, dockerHub, ghcr resolver.Resolver) (string, error) {
	matches := copyFromRegex.FindStringSubmatch(instr.Args)
	if matches == nil {
		return "", fmt.Errorf("invalid COPY --from format")
	}

	image := matches[1]

	imageMatches := imageRegex.FindStringSubmatch(image)
	if imageMatches == nil {
		return "", fmt.Errorf("invalid image format: %s", image)
	}

	imageName := imageMatches[1]

	if len(directive.Packages) == 0 {
		return "", fmt.Errorf("no package directive found")
	}

	pkg := directive.Packages[0]
	if pkg.Ignore {
		return "", nil
	}

	var r resolver.Resolver
	if strings.Contains(imageName, "ghcr.io") {
		r = ghcr
	} else {
		r = dockerHub
	}

	latest, err := r.Resolve(ctx, imageName, pkg.Pattern)
	if err != nil {
		return "", err
	}

	return latest, nil
}
