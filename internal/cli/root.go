package cli

import (
	"context"
	"log/slog"
	"os"

	"github.com/urfave/cli/v3"

	"github.com/kyleking/doner/internal/httpclient"
)

func NewApp(version, commit, date string) *cli.Command {
	return &cli.Command{
		Name:    "doner",
		Usage:   "Dockerfile version maintainer",
		Version: version,
		Before:  setupLogging,
		Flags: []cli.Flag{
			&cli.BoolFlag{
				Name:  "verbose",
				Usage: "verbose output (debug logging)",
			},
		},
		Commands: []*cli.Command{
			newCheckCommand(),
			newUpdateCommand(),
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
