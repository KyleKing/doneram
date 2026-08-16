package cli

import (
	"context"
	"fmt"

	"github.com/urfave/cli/v3"
)

func newVersionCommand(version, commit, date string) *cli.Command {
	return &cli.Command{
		Name:  "version",
		Usage: "Print version information",
		Action: func(ctx context.Context, cmd *cli.Command) error {
			fmt.Printf("doneram %s\n", version)
			fmt.Printf("  commit: %s\n", commit)
			fmt.Printf("  built:  %s\n", date)
			return nil
		},
	}
}
