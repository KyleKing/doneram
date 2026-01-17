package main

import (
	"context"
	"os"

	"github.com/kyleking/doner/internal/cli"
)

var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

func main() {
	app := cli.NewApp(version, commit, date)
	if err := app.Run(context.Background(), os.Args); err != nil {
		os.Exit(1)
	}
}
