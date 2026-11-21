package cli

import "fmt"

// Exit codes, so a workflow can tell a broken config from a flaky registry
// from a pin that simply needs updating. When a run hits several, the most
// actionable wins, in this order: config, patch, match-count, unresolved,
// vulnerable, drift.
const (
	// ExitConfig: the config could not be loaded or names a tool that
	// does not exist.
	ExitConfig = 2
	// ExitMismatch: a site's pattern matched a different number of times
	// than it expects, so the pin may have moved.
	ExitMismatch = 3
	// ExitUnresolved: a resolver could not answer, usually a registry or
	// network problem worth retrying.
	ExitUnresolved = 4
	// ExitPatch: a file could not be patched.
	ExitPatch = 5
	// ExitDrift: --fail-on-drift and a pin is out of date.
	ExitDrift = 6
	// ExitVulnerable: --fail-on-vuln and a pin with a known advisory is
	// still on disk.
	ExitVulnerable = 7
	// ExitUnpinned: --fail-on-unpinned (or `pin --check`) and an action
	// or tool still names a moving target.
	ExitUnpinned = 8
)

// ExitError carries the process exit code for an error. main reads it
// through the ExitCode method, which urfave/cli's ExitCoder also uses.
type ExitError struct {
	Code int
	Err  error
}

func (e *ExitError) Error() string { return e.Err.Error() }
func (e *ExitError) Unwrap() error { return e.Err }
func (e *ExitError) ExitCode() int { return e.Code }

func exitf(code int, format string, a ...any) error {
	return &ExitError{Code: code, Err: fmt.Errorf(format, a...)}
}
