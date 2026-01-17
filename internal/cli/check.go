package cli

import (
	"context"
	"fmt"
	"os"

	"github.com/kyleking/doner/internal/parser"
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
				Value:   "Dockerfile",
				Usage:   "path to Dockerfile",
			},
		},
		Action: runCheck,
	}
}

func runCheck(ctx context.Context, cmd *cli.Command) error {
	file := cmd.String("file")
	verbose := cmd.Bool("verbose")

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

	fmt.Printf("Parsed %s:\n", file)
	fmt.Printf("  Instructions: %d\n", len(df.Instructions))
	fmt.Printf("  Directives:   %d\n", len(df.Directives))

	for _, d := range df.Directives {
		fmt.Printf("\n  Line %d: # doner: %s\n", d.Line, d.Raw)
		for _, pkg := range d.Packages {
			if pkg.Ignore {
				fmt.Printf("    - %s: ignore\n", pkg.Name)
			} else {
				fmt.Printf("    - %s: %s\n", pkg.Name, pkg.Pattern.String())
			}
		}
	}

	return nil
}
