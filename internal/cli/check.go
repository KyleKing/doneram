package cli

import (
	"context"
	"fmt"
	"os"
	"regexp"
	"strings"

	"github.com/kyleking/doner/internal/parser"
	"github.com/kyleking/doner/internal/reporter"
	"github.com/kyleking/doner/internal/resolver"
	"github.com/kyleking/doner/internal/updater"
	"github.com/urfave/cli/v3"
)

func newCheckCommand() *cli.Command {
	return &cli.Command{
		Name:  "check",
		Usage: "Check for available updates (dry-run)",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:    "file",
				Aliases: []string{"f"},
				Usage:   "path to Dockerfile (e.g., Dockerfile, docker/api/Dockerfile)",
			},
		},
		Action: runCheck,
	}
}

func runCheck(ctx context.Context, cmd *cli.Command) error {
	file := cmd.String("file")
	if file == "" {
		file = "Dockerfile"
	}
	verbose := cmd.Bool("verbose")

	rep := reporter.NewReporter(verbose)

	if verbose {
		fmt.Printf("Checking %s for updates...\n", file)
	}

	content, err := os.ReadFile(file)
	if err != nil {
		return fmt.Errorf("reading dockerfile: %w", err)
	}

	df, err := parser.Parse(string(content))
	if err != nil {
		return fmt.Errorf("parsing dockerfile: %w", err)
	}

	dockerHub := resolver.NewDockerHubResolver()
	ghcr := resolver.NewGHCRResolver()

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
			if verbose {
				fmt.Printf("  Error: %v\n", err)
			}
			continue
		}

		if latest != "" {
			resolved[instr.Line] = latest
		}
	}

	// Create updates list
	updates := updater.UpdateFromInstructions(df.Instructions, directiveMap, resolved)

	// Report results
	rep.ReportCheck(file, len(df.Instructions), len(df.Directives), updates)

	return nil
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
