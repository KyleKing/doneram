package cli

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/kyleking/doner/internal/builder"
	"github.com/kyleking/doner/internal/parser"
	"github.com/kyleking/doner/internal/reporter"
	"github.com/kyleking/doner/internal/resolver"
	"github.com/kyleking/doner/internal/updater"
	"github.com/urfave/cli/v3"
)

func newUpdateCommand() *cli.Command {
	return &cli.Command{
		Name:  "update",
		Usage: "Update Dockerfile with latest versions and validate",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:    "file",
				Aliases: []string{"f"},
				Usage:   "path to Dockerfile (e.g., Dockerfile, docker/api/Dockerfile)",
			},
			&cli.BoolFlag{
				Name:  "skip-build",
				Usage: "skip Docker build validation",
			},
			&cli.BoolFlag{
				Name:  "skip-healthcheck",
				Usage: "skip healthcheck validation",
			},
		},
		Action: runUpdate,
	}
}

func runUpdate(ctx context.Context, cmd *cli.Command) error {
	file := cmd.String("file")
	if file == "" {
		file = "Dockerfile"
	}
	verbose := cmd.Bool("verbose")
	skipBuild := cmd.Bool("skip-build")
	skipHealthcheck := cmd.Bool("skip-healthcheck")

	rep := reporter.NewReporter(verbose)

	// Parse Dockerfile
	content, err := os.ReadFile(file)
	if err != nil {
		return fmt.Errorf("reading dockerfile: %w", err)
	}

	df, err := parser.Parse(string(content))
	if err != nil {
		return fmt.Errorf("parsing dockerfile: %w", err)
	}

	// Create resolvers
	dockerHub := resolver.NewDockerHubResolver()
	ghcr := resolver.NewGHCRResolver()

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
		if instr.Command == "FROM" {
			latest, err = resolveFromInstruction(ctx, instr, directive, dockerHub, ghcr)
		} else if instr.Command == "COPY" {
			latest, err = resolveCopyFromInstruction(ctx, instr, directive, dockerHub, ghcr)
		}

		if err != nil {
			if verbose {
				fmt.Printf("  Warning: %v\n", err)
			}
			continue
		}

		if latest != "" {
			resolved[instr.Line] = latest
		}
	}

	// Create updates
	updates := updater.UpdateFromInstructions(df.Instructions, directiveMap, resolved)

	if len(updates) == 0 {
		fmt.Printf("No updates available for %s\n", file)
		return nil
	}

	// Apply updates
	u := updater.NewUpdater(string(content))
	if err := u.Apply(updates); err != nil {
		return fmt.Errorf("applying updates: %w", err)
	}

	// Write updated Dockerfile
	if err := os.WriteFile(file, []byte(u.Content()), 0644); err != nil {
		return fmt.Errorf("writing updated dockerfile: %w", err)
	}

	// Build and validate if not skipped
	var buildSuccess bool
	var buildError error

	if !skipBuild {
		bldr := builder.NewDockerBuilder(verbose)

		if verbose {
			fmt.Printf("\nBuilding %s...\n", file)
		}

		imageID, err := bldr.Build(ctx, file)
		if err != nil {
			buildError = err
			rep.ReportUpdate(file, updates, false, buildError)
			return fmt.Errorf("build validation failed: %w", err)
		}

		buildSuccess = true

		// Validate with healthcheck if present and not skipped
		if !skipHealthcheck {
			healthcheck := builder.ExtractHealthcheck(df)
			if healthcheck != nil {
				if verbose {
					fmt.Println("Running healthcheck validation...")
				}

				if err := bldr.Validate(ctx, imageID, healthcheck); err != nil {
					rep.ReportValidation(imageID, false, err)
					_ = bldr.Cleanup(ctx, imageID)
					return fmt.Errorf("healthcheck validation failed: %w", err)
				}

				rep.ReportValidation(imageID, true, nil)
			}
		}

		// Cleanup built image
		if err := bldr.Cleanup(ctx, imageID); err != nil {
			if verbose {
				fmt.Printf("Warning: failed to cleanup image: %v\n", err)
			}
		}
	}

	rep.ReportUpdate(file, updates, buildSuccess, buildError)
	return nil
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
