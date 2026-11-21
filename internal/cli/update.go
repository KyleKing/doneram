package cli

import (
	"context"

	"github.com/urfave/cli/v3"
)

func newUpdateCommand() *cli.Command {
	flags := append(runFlags(),
		&cli.StringFlag{
			Name:  "plan",
			Usage: "a summary JSON from an earlier update run; with --group, apply only that group's changes",
		},
		&cli.StringFlag{
			Name:  "group",
			Usage: "the group in --plan to apply (updates, review-<tool>, failing-<tool>)",
		},
	)
	return &cli.Command{
		Name:  "update",
		Usage: "Patch every out-of-date pin, relock, and run the config's validate command",
		Flags: flags,
		Action: func(ctx context.Context, cmd *cli.Command) error {
			var group *pklGroup
			if id := cmd.String("group"); id != "" || cmd.String("plan") != "" {
				if id == "" || cmd.String("plan") == "" {
					return exitf(ExitConfig, "--plan and --group go together")
				}
				g, err := loadGroup(cmd.String("plan"), id)
				if err != nil {
					return &ExitError{Code: ExitConfig, Err: err}
				}
				group = g
			}
			return runFromFlags(ctx, cmd, true, group)
		},
	}
}
