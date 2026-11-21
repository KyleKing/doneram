package cli

import (
	"context"
	"fmt"
	"io"
	"os/exec"
	"strings"

	"github.com/kyleking/doneram/internal/config"
	"github.com/kyleking/doneram/internal/engine"
	"github.com/kyleking/doneram/internal/harden"
)

// maxOffenders bounds how many failing bumps bisection hunts for before it
// gives up and lets validation fail. Each costs about log2(n) runs.
const maxOffenders = 3

// trialFunc applies only the given tools' bumps (plus every change that is
// not a bump) to a clean tree, validates, and reports whether it passed and
// what it printed.
type trialFunc func(tools []string) (bool, string)

// bisect finds the bumps that make validation fail, assuming validation
// has already failed with every unit applied. It first checks the tree
// passes with no bumps at all; if not, no bump is to blame and baseline is
// the failure. Otherwise it finds an offender by halving a failing prefix,
// sets it aside, and repeats until the rest passes or maxOffenders are
// found. offenders maps each to the output of the smallest run that failed
// with it.
func bisect(units []string, trial trialFunc) (offenders map[string]string, baseline string) {
	if ok, output := trial(nil); !ok {
		return nil, output
	}
	offenders = map[string]string{}
	remaining := append([]string(nil), units...)
	for len(offenders) < maxOffenders && len(remaining) > 0 {
		if ok, _ := trial(remaining); ok {
			break
		}
		// trial(remaining[:hi]) fails and trial(remaining[:lo]) passes, so
		// remaining[hi-1] is the first bump the failure needs.
		lo, hi := 0, len(remaining)
		failure := ""
		for hi-lo > 1 {
			mid := (lo + hi) / 2
			if ok, output := trial(remaining[:mid]); ok {
				lo = mid
			} else {
				hi, failure = mid, output
			}
		}
		if failure == "" {
			_, failure = trial(remaining[:hi])
		}
		offender := remaining[hi-1]
		offenders[offender] = failure
		remaining = append(remaining[:hi-1:hi-1], remaining[hi:]...)
	}
	return offenders, ""
}

// runValidate runs a repo's validate command from baseDir.
func runValidate(ctx context.Context, command, baseDir string) (bool, string) {
	cmd := exec.CommandContext(ctx, "sh", "-c", command)
	cmd.Dir = baseDir
	out, err := cmd.CombinedOutput()
	output := tail(string(out), 40)
	if err != nil && strings.TrimSpace(output) == "" {
		output = fmt.Sprintf("(%s printed nothing; %v)", command, err)
	}
	return err == nil, output
}

func tail(s string, lines int) string {
	parts := strings.Split(strings.TrimRight(s, "\n"), "\n")
	if len(parts) > lines {
		parts = append([]string{fmt.Sprintf("... %d lines omitted", len(parts)-lines)}, parts[len(parts)-lines:]...)
	}
	return strings.Join(parts, "\n")
}

// gitSnapshot records the working tree as it is now, uncommitted changes
// included, so a trial can return to it. It is "HEAD" for a clean tree.
func gitSnapshot(ctx context.Context, dir string) (string, error) {
	out, err := exec.CommandContext(ctx, "git", "-C", dir, "stash", "create").Output()
	if err != nil {
		return "", fmt.Errorf("bisecting needs a git checkout: %w", err)
	}
	if ref := strings.TrimSpace(string(out)); ref != "" {
		return ref, nil
	}
	return "HEAD", nil
}

func gitRestore(ctx context.Context, dir, ref string) error {
	if out, err := exec.CommandContext(ctx, "git", "-C", dir, "checkout", ref, "--", ".").CombinedOutput(); err != nil {
		return fmt.Errorf("restoring %s: %w: %s", ref, err, strings.TrimSpace(string(out)))
	}
	return nil
}

// validateAndBisect runs validate over a plan run's applied tree. When it
// fails, every bump is replayed in subsets from snapshot to find the ones
// to blame, and the tree is left with only the bumps that pass. It returns
// the offenders, and the output of a failure no bump explains: one that
// happens with no bump at all, or that remains once every offender found
// is set aside.
func validateAndBisect(ctx context.Context, out report, cfg *config.Config, baseDir, snapshot string, results []engine.SiteResult, summary pklSummary, locks []pklLockSummary, pin harden.Resolver) (map[string]string, string) {
	ok, output := runValidate(ctx, cfg.Validate, baseDir)
	if ok {
		out.printf("\nvalidate passed (%s)\n", cfg.Validate)
		return nil, ""
	}
	out.printf("\nvalidate failed (%s):\n%s\n", cfg.Validate, output)
	if snapshot == "" {
		return nil, output
	}

	var units []string
	seen := map[string]bool{}
	for _, s := range summary.Results {
		if s.Updated && !seen[s.Tool] {
			seen[s.Tool] = true
			units = append(units, s.Tool)
		}
	}
	quiet := report{w: io.Discard}
	trial := func(tools []string) (bool, string) {
		if err := gitRestore(ctx, baseDir, snapshot); err != nil {
			return false, err.Error()
		}
		want := map[string]bool{}
		for _, t := range tools {
			want[t] = true
		}
		for _, r := range results {
			if want[r.Site.Tool] && r.Err == nil && siteNeedsPatch(r) {
				if _, err := patchSite(r); err != nil {
					return false, err.Error()
				}
			}
		}
		lockCopy := append([]pklLockSummary(nil), locks...)
		applyLockFixes(ctx, quiet, lockCopy)
		if _, _, err := refreshDigests(ctx, quiet, baseDir, pin, nil); err != nil {
			return false, err.Error()
		}
		if cfg.AfterPatch != "" {
			if err := runAfterPatch(ctx, quiet, cfg.AfterPatch, baseDir); err != nil {
				return false, err.Error()
			}
		}
		return runValidate(ctx, cfg.Validate, baseDir)
	}

	offenders, baseline := bisect(units, trial)
	if baseline != "" {
		out.printf("validate fails with no pin updated, so no bump is to blame\n")
		// Leave the full update in place for the PR to show.
		trial(units)
		return nil, baseline
	}
	var good []string
	for _, u := range units {
		if _, bad := offenders[u]; !bad {
			good = append(good, u)
		}
	}
	ok, output = trial(good)
	for tool := range offenders {
		out.printf("✗ %s breaks validate; split into its own pull request\n", tool)
	}
	if ok {
		output = ""
	}
	return offenders, output
}
