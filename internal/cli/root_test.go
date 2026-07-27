package cli

import (
	"context"
	"log/slog"
	"testing"

	"github.com/urfave/cli/v3"

	"github.com/kyleking/doner/internal/httpclient"
)

func TestNewAppWiresCommands(t *testing.T) {
	app := NewApp("1.2.3", "abc123", "2026-01-01")

	if app.Name != "doner" {
		t.Errorf("app.Name = %q, want doner", app.Name)
	}
	if app.Version != "1.2.3" {
		t.Errorf("app.Version = %q, want 1.2.3", app.Version)
	}

	want := map[string]bool{"check": false, "update": false, "version": false}
	for _, cmd := range app.Commands {
		if _, ok := want[cmd.Name]; ok {
			want[cmd.Name] = true
		}
	}
	for name, found := range want {
		if !found {
			t.Errorf("command %q is not registered", name)
		}
	}
}

func TestSetupLoggingAttachesLogger(t *testing.T) {
	for _, verbose := range []bool{false, true} {
		cmd := &cli.Command{
			Flags:  []cli.Flag{&cli.BoolFlag{Name: "verbose"}},
			Action: func(context.Context, *cli.Command) error { return nil },
		}
		args := []string{"doner"}
		if verbose {
			args = append(args, "--verbose")
		}
		if err := cmd.Run(context.Background(), args); err != nil {
			t.Fatalf("cmd.Run: %v", err)
		}

		ctx, err := setupLogging(context.Background(), cmd)
		if err != nil {
			t.Fatalf("setupLogging: %v", err)
		}

		logger := httpclient.LoggerFromContext(ctx)
		if logger == slog.Default() {
			t.Fatalf("verbose=%v: setupLogging did not attach a logger to the context", verbose)
		}
		if got := logger.Enabled(ctx, slog.LevelDebug); got != verbose {
			t.Errorf("verbose=%v: debug logging enabled = %v", verbose, got)
		}
	}
}

func TestVersionCommandRuns(t *testing.T) {
	app := NewApp("1.2.3", "abc123", "2026-01-01")
	if err := app.Run(context.Background(), []string{"doner", "version"}); err != nil {
		t.Fatalf("running version: %v", err)
	}
}

func TestCheckCommandDefinesFlags(t *testing.T) {
	cmd := newCheckCommand()
	if cmd.Name != "check" {
		t.Fatalf("cmd.Name = %q, want check", cmd.Name)
	}
	assertHasFlags(t, cmd, "file", "format", "workers")
}

func TestUpdateCommandDefinesFlags(t *testing.T) {
	cmd := newUpdateCommand()
	if cmd.Name != "update" {
		t.Fatalf("cmd.Name = %q, want update", cmd.Name)
	}
	assertHasFlags(t, cmd, "file", "format", "workers", "skip-build", "skip-healthcheck")
}

func assertHasFlags(t *testing.T, cmd *cli.Command, names ...string) {
	t.Helper()
	have := make(map[string]bool)
	for _, f := range cmd.Flags {
		for _, n := range f.Names() {
			have[n] = true
		}
	}
	for _, n := range names {
		if !have[n] {
			t.Errorf("command %q is missing flag %q", cmd.Name, n)
		}
	}
}
