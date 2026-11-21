package cli

import (
	"context"
	"fmt"

	"github.com/urfave/cli/v3"

	"github.com/kyleking/doneram/internal/resolver"
)

func newCheckCommand() *cli.Command {
	return &cli.Command{
		Name:   "check",
		Usage:  "Report out-of-date, vulnerable, floating, and end-of-life pins without changing anything",
		Flags:  runFlags(),
		Action: func(ctx context.Context, cmd *cli.Command) error { return runFromFlags(ctx, cmd, false, nil) },
	}
}

// runFlags are the flags check and update share: what to read, how to
// report it, and which findings fail the run.
func runFlags() []cli.Flag {
	return []cli.Flag{
		&cli.StringFlag{
			Name:  "format",
			Usage: "output format: stdout or json",
			Value: "stdout",
		},
		&cli.IntFlag{
			Name:  "workers",
			Usage: "number of sites resolved in parallel",
			Value: 8,
		},
		&cli.BoolFlag{
			Name:  "fail-on-drift",
			Usage: "exit non-zero when a pin is out of date, instead of reporting drift and exiting 0",
		},
		&cli.StringFlag{
			Name:  "snapshot",
			Usage: "write a GitHub dependency submission snapshot of the pins on disk to this path",
		},
		&cli.BoolFlag{
			Name:  "fail-on-eol",
			Usage: "exit 9 when a runtime, base image, runner, language floor, or model is past end of life",
		},
		&cli.BoolFlag{
			Name:  "fail-on-unpinned",
			Usage: "exit 8 when an action is used by tag or a mise tool by prefix after the run",
		},
		&cli.BoolFlag{
			Name:  "fail-on-vuln",
			Usage: "exit 7 when a pin with a known advisory is still on disk after the run",
		},
		&cli.DurationFlag{
			Name:  "min-age",
			Usage: "hold back a release until it is this old, matching Dependabot's three-day default; 0 disables",
			Value: resolver.DefaultCooldown,
		},
		&cli.StringFlag{
			Name:  "config",
			Usage: "path to a .doneram.pkl config, overriding discovery in the working directory",
		},
		&cli.StringSliceFlag{
			Name:  "only",
			Usage: "check only these tools (repeatable)",
		},
		&cli.StringFlag{
			Name:  "output",
			Usage: "write the run's JSON summary to this path",
		},
	}
}

// runFromFlags runs the repo in the working directory: its .doneram.pkl
// when there is one (or --config names one), and otherwise what discovery
// finds, Dockerfile directives included.
func runFromFlags(ctx context.Context, cmd *cli.Command, apply bool, group *pklGroup) error {
	ctx = resolver.ContextWithCooldown(ctx, cmd.Duration("min-age"))
	run := pklRun{
		apply:          apply,
		output:         cmd.String("output"),
		workers:        int(cmd.Int("workers")),
		only:           cmd.StringSlice("only"),
		format:         cmd.String("format"),
		failOnDrift:    cmd.Bool("fail-on-drift"),
		failOnVuln:     cmd.Bool("fail-on-vuln"),
		failOnUnpinned: cmd.Bool("fail-on-unpinned"),
		failOnEOL:      cmd.Bool("fail-on-eol"),
		snapshot:       cmd.String("snapshot"),
		group:          group,
	}
	if path, ok := findDoneramConfig(cmd.String("config")); ok {
		run.path = path
	} else if path := cmd.String("config"); path != "" {
		return &ExitError{Code: ExitConfig, Err: fmt.Errorf("reading %s: no such config", path)}
	}
	return runCheckPkl(ctx, run)
}
