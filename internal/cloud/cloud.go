// Package cloud runs a cloud provider's own CLI (aws, gcloud) for the
// read-only lookups doneram makes against an account: newest AMI, a public
// SSM parameter, a GKE release channel, a registry login. The CLI carries
// the credentials, profile, and region the person already uses, so doneram
// never handles a key. A missing CLI or missing credentials is reported as
// ErrUnavailable, which a run treats as "skipped", not as a failure.
package cloud

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

// ErrUnavailable marks a lookup that could not run here: the CLI is not
// installed, or it has no credentials. The pin is reported as skipped.
var ErrUnavailable = errors.New("cloud lookup unavailable")

// Runner runs a CLI and returns its trimmed stdout.
type Runner func(ctx context.Context, name string, args ...string) (string, error)

// noCredentials are the messages aws and gcloud print when they have no
// usable identity, as opposed to a real error from the API.
var noCredentials = []string{
	"Unable to locate credentials",
	"The config profile",
	"ExpiredToken",
	"Token has expired",
	"You must specify a region",
	"You do not currently have an active account selected",
	"Reauthentication failed",
	"The required property [project] is not currently set",
	"The required property [account] is not currently set",
	"Please run:\n\n  $ gcloud auth login",
}

// Run is the Runner that executes the real CLI.
func Run(ctx context.Context, name string, args ...string) (string, error) {
	if _, err := exec.LookPath(name); err != nil {
		return "", fmt.Errorf("%w: %s is not installed", ErrUnavailable, name)
	}
	cmd := exec.CommandContext(ctx, name, args...)
	out, err := cmd.Output()
	if err != nil {
		stderr := ""
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			stderr = strings.TrimSpace(string(exit.Stderr))
		}
		for _, m := range noCredentials {
			if strings.Contains(stderr, m) {
				return "", fmt.Errorf("%w: %s has no usable credentials (%s)", ErrUnavailable, name, firstLine(stderr))
			}
		}
		return "", fmt.Errorf("%s %s: %w: %s", name, args[0], err, firstLine(stderr))
	}
	return strings.TrimSpace(string(out)), nil
}

func firstLine(s string) string {
	line, _, _ := strings.Cut(strings.TrimSpace(s), "\n")
	return line
}
