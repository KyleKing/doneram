package cli

import (
	"context"
	"log/slog"
	"os"

	"github.com/urfave/cli/v3"

	"github.com/kyleking/doneram/internal/httpclient"
)

func NewApp(version, commit, date string) *cli.Command {
	appVersion = version
	return &cli.Command{
		Name:    "doneram",
		Usage:   "Keep pinned versions current: find, resolve, patch, and validate them",
		Version: version,
		Before:  setupLogging,
		// main picks the exit code from the returned error; urfave/cli's
		// default would print it and os.Exit from inside Run, which also
		// kills a test that drives the app.
		ExitErrHandler: func(context.Context, *cli.Command, error) {},
		Flags: []cli.Flag{
			&cli.BoolFlag{
				Name:  "verbose",
				Usage: "verbose output (debug logging)",
			},
		},
		Commands: []*cli.Command{
			newCheckCommand(),
			newUpdateCommand(),
			newPinCommand(),
			newFleetCommand(),
			newExplainCommand(),
			newVersionCommand(version, commit, date),
		},
	}
}

func setupLogging(ctx context.Context, cmd *cli.Command) (context.Context, error) {
	level := slog.LevelInfo
	if cmd.Bool("verbose") {
		level = slog.LevelDebug
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level}))
	return httpclient.ContextWithLogger(ctx, logger), nil
}
