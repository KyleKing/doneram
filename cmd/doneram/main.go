package main

import (
	"context"
	"log/slog"
	"os"

	"github.com/kyleking/doneram/internal/cli"
	"github.com/kyleking/doneram/internal/httpclient"
)

var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))

	ctx := httpclient.ContextWithLogger(context.Background(), logger)

	app := cli.NewApp(version, commit, date)
	if err := app.Run(ctx, os.Args); err != nil {
		logger.Error("application error", "error", err)
		os.Exit(1)
	}
}
