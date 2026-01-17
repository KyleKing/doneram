package cli

import (
	"github.com/urfave/cli/v3"
)

func NewApp(version, commit, date string) *cli.Command {
	return &cli.Command{
		Name:    "doner",
		Usage:   "Dockerfile version maintainer",
		Version: version,
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:    "config",
				Aliases: []string{"c"},
				Usage:   "config file (default is .doner.yml)",
			},
			&cli.BoolFlag{
				Name:  "verbose",
				Usage: "verbose output",
			},
		},
		Commands: []*cli.Command{
			newCheckCommand(),
			newUpdateCommand(),
			newVersionCommand(version, commit, date),
		},
	}
}
