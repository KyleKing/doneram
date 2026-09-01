package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/urfave/cli/v3"

	"github.com/kyleking/doneram/internal/builder"
	"github.com/kyleking/doneram/internal/httpclient"
	"github.com/kyleking/doneram/internal/parser"
	"github.com/kyleking/doneram/internal/reporter"
	"github.com/kyleking/doneram/internal/resolver"
	"github.com/kyleking/doneram/internal/updater"
)

func newUpdateCommand() *cli.Command {
	return &cli.Command{
		Name:  "update",
		Usage: "Update Dockerfile with latest versions and validate",
		Flags: []cli.Flag{
			&cli.StringSliceFlag{
				Name:    "file",
				Aliases: []string{"f"},
				Usage:   "path(s) to Dockerfile - supports globs (e.g., docker/*/Dockerfile, **/Dockerfile)",
			},
			&cli.BoolFlag{
				Name:  "skip-build",
				Usage: "skip Docker build validation",
			},
			&cli.BoolFlag{
				Name:  "skip-healthcheck",
				Usage: "skip healthcheck validation",
			},
			&cli.StringFlag{
				Name:  "format",
				Usage: "output format: stdout, github-actions, json",
				Value: "stdout",
			},
			&cli.IntFlag{
				Name:  "workers",
				Usage: "number of sites or files resolved in parallel",
				Value: 8,
			},
			&cli.StringFlag{
				Name:  "output",
				Usage: "write a .doneram.pkl config's JSON summary to this path",
			},
		},
		Action: runUpdate,
	}
}

func runUpdate(ctx context.Context, cmd *cli.Command) error {
	patterns := cmd.StringSlice("file")
	verbose := cmd.Bool("verbose")
	skipBuild := cmd.Bool("skip-build")
	skipHealthcheck := cmd.Bool("skip-healthcheck")
	format := cmd.String("format")
	workers := cmd.Int("workers")

	if len(patterns) == 0 {
		if path, ok := findDoneramConfig(); ok {
			return runCheckPkl(ctx, pklRun{path: path, apply: true, output: cmd.String("output"), workers: int(cmd.Int("workers"))})
		}
	}

	// Expand file patterns
	files, err := expandFiles(patterns)
	if err != nil {
		return err
	}

	rep := reporter.NewOutputReporter(format, verbose)

	if verbose {
		fmt.Printf("Updating %d file(s)...\n", len(files))
	}

	// Process files in parallel
	cfg := ProcessorConfig{
		MaxWorkers:      int(workers),
		Verbose:         verbose,
		SkipBuild:       skipBuild,
		SkipHealthcheck: skipHealthcheck,
	}

	results := processFilesParallel(ctx, files, cfg, processUpdateFile)

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

		if len(result.Updates) == 0 {
			fmt.Printf("No updates available for %s\n", result.File)
			successCnt++
			continue
		}

		if result.BuildError != nil {
			failedCnt++
		} else {
			successCnt++
		}

		totalUpdates += len(result.Updates)
		rep.ReportUpdate(result.File, result.Updates, result.BuildSuccess, result.BuildError)
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

// processUpdateFile processes a single Dockerfile update and returns the result.
func processUpdateFile(ctx context.Context, file string, cfg ProcessorConfig) FileResult {
	logger := httpclient.LoggerFromContext(ctx)
	result := FileResult{
		File: file,
	}

	logger.Info("updating file", "file", file, "format", "dockerfile")

	// Parse Dockerfile
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

	// Create resolvers
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

	// Build directive map
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
		switch instr.Command {
		case "FROM":
			latest, err = resolveFromInstruction(ctx, instr, directive, dockerHub, ghcr)
		case "COPY":
			latest, err = resolveCopyFromInstruction(ctx, instr, directive, dockerHub, ghcr)
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

	// Create updates
	result.Updates = updater.UpdateFromInstructions(df.Instructions, directiveMap, resolved)

	if len(result.Updates) == 0 {
		logger.Info("update completed", "file", file, "updates_found", 0)
		return result
	}

	logger.Info("updates found", "file", file, "count", len(result.Updates))

	// Apply updates
	u := updater.NewUpdater(string(content))
	if err := u.Apply(result.Updates); err != nil {
		result.ProcessingErr = fmt.Errorf("applying updates: %w", err)
		return result
	}

	// Write updated Dockerfile
	if err := os.WriteFile(file, []byte(u.Content()), 0644); err != nil {
		result.ProcessingErr = fmt.Errorf("writing updated dockerfile: %w", err)
		return result
	}

	logger.Info("update completed", "file", file, "updates_applied", len(result.Updates))

	// Build and validate if not skipped
	if !cfg.SkipBuild {
		bldr := builder.NewDockerBuilder(cfg.Verbose)

		if cfg.Verbose {
			fmt.Printf("\nBuilding %s...\n", file)
		}

		imageID, err := bldr.Build(ctx, file)
		if err != nil {
			result.BuildError = err
			return result
		}

		result.BuildSuccess = true

		// Validate with healthcheck if present and not skipped
		if !cfg.SkipHealthcheck {
			healthcheck := builder.ExtractHealthcheck(df)
			if healthcheck != nil {
				if cfg.Verbose {
					fmt.Println("Running healthcheck validation...")
				}

				if err := bldr.Validate(ctx, imageID, healthcheck); err != nil {
					_ = bldr.Cleanup(ctx, imageID)
					result.BuildError = err
					result.BuildSuccess = false
					return result
				}
			}
		}

		// Cleanup built image
		if err := bldr.Cleanup(ctx, imageID); err != nil {
			if cfg.Verbose {
				fmt.Printf("Warning: failed to cleanup image: %v\n", err)
			}
		}
	}

	return result
}

func resolveFromInstruction(ctx context.Context, instr parser.Instruction, directive *parser.Directive, dockerHub, ghcr resolver.Resolver) (string, error) {
	imageName, currentVersion := parseImageFromArgs(instr.Args)
	if imageName == "" || currentVersion == "" {
		return "", fmt.Errorf("invalid FROM format: %s", instr.Args)
	}

	if len(directive.Packages) == 0 {
		return "", fmt.Errorf("no package directive found")
	}

	pkg := directive.Packages[0]
	if pkg.Ignore {
		return "", nil
	}

	var r resolver.Resolver
	if containsGHCR(imageName) {
		r = ghcr
	} else {
		r = dockerHub
	}

	return r.Resolve(ctx, imageName, pkg.Pattern)
}

func resolveCopyFromInstruction(ctx context.Context, instr parser.Instruction, directive *parser.Directive, dockerHub, ghcr resolver.Resolver) (string, error) {
	imageName, currentVersion := parseImageFromCopyArgs(instr.Args)
	if imageName == "" || currentVersion == "" {
		return "", nil // Not a --from= with image reference
	}

	if len(directive.Packages) == 0 {
		return "", fmt.Errorf("no package directive found")
	}

	pkg := directive.Packages[0]
	if pkg.Ignore {
		return "", nil
	}

	var r resolver.Resolver
	if containsGHCR(imageName) {
		r = ghcr
	} else {
		r = dockerHub
	}

	return r.Resolve(ctx, imageName, pkg.Pattern)
}

func containsGHCR(imageName string) bool {
	return strings.Contains(imageName, "ghcr.io")
}

func parseImageFromArgs(args string) (string, string) {
	// Remove AS alias if present
	image := args
	if idx := strings.Index(image, " AS "); idx != -1 {
		image = image[:idx]
	}
	if idx := strings.Index(image, " as "); idx != -1 {
		image = image[:idx]
	}

	image = strings.TrimSpace(image)

	// Split by colon (last occurrence)
	idx := strings.LastIndex(image, ":")
	if idx == -1 {
		return "", ""
	}

	return image[:idx], image[idx+1:]
}

func parseImageFromCopyArgs(args string) (string, string) {
	// Find --from= flag
	fromIdx := strings.Index(args, "--from=")
	if fromIdx == -1 {
		return "", ""
	}

	// Extract image reference after --from=
	rest := args[fromIdx+7:]
	spaceIdx := strings.Index(rest, " ")
	if spaceIdx == -1 {
		spaceIdx = len(rest)
	}

	image := rest[:spaceIdx]

	// Split by colon (last occurrence)
	colonIdx := strings.LastIndex(image, ":")
	if colonIdx == -1 {
		return "", ""
	}

	return image[:colonIdx], image[colonIdx+1:]
}
