package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/urfave/cli/v3"

	"github.com/kyleking/doneram/internal/fleet"
	"github.com/kyleking/doneram/internal/harden"
	"github.com/kyleking/doneram/internal/resolver"
)

func newFleetCommand() *cli.Command {
	return &cli.Command{
		Name:  "fleet",
		Usage: "Run doneram across the local git repos in a directory: one report, or with --apply a commit on " + fleet.Branch + " in each",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:  "dir",
				Usage: "the directory whose child git repos are covered",
				Value: ".",
			},
			&cli.StringFlag{
				Name:  "match",
				Usage: "cover only child directories whose name matches this regex",
				Value: ".*",
			},
			&cli.StringFlag{
				Name:  "exclude",
				Usage: "leave out child directories whose name matches this regex",
			},
			&cli.IntFlag{
				Name:  "workers",
				Usage: "number of repos covered in parallel",
				Value: 4,
			},
			&cli.BoolFlag{
				Name:  "apply",
				Usage: "update each clean repo and commit the result on " + fleet.Branch + "; the checked-out branch is left alone and nothing is pushed",
			},
			&cli.StringFlag{
				Name:  "report",
				Usage: "also write the markdown report to this path",
			},
			&cli.StringFlag{
				Name:  "json",
				Usage: "write the report as JSON to this path",
			},
		},
		Action: runFleet,
	}
}

func runFleet(ctx context.Context, cmd *cli.Command) error {
	match, err := regexp.Compile(cmd.String("match"))
	if err != nil {
		return exitf(ExitConfig, "--match: %v", err)
	}
	var exclude *regexp.Regexp
	if s := cmd.String("exclude"); s != "" {
		if exclude, err = regexp.Compile(s); err != nil {
			return exitf(ExitConfig, "--exclude: %v", err)
		}
	}
	dir := cmd.String("dir")
	repos, err := fleet.Repos(dir, match, exclude)
	if err != nil {
		return &ExitError{Code: ExitConfig, Err: err}
	}
	if len(repos) == 0 {
		return exitf(ExitConfig, "no git repos under %s match --match %q", dir, match)
	}
	ctx = resolver.ContextWithCooldown(ctx, resolver.DefaultCooldown)
	report := fleet.Run(ctx, repos, fleet.Options{Apply: cmd.Bool("apply"), Workers: int(cmd.Int("workers")), Log: os.Stderr}, fleetCheck)
	return writeFleetReport(report, os.Stdout, cmd.String("report"), cmd.String("json"))
}

// writeFleetReport prints the markdown and writes the files asked for.
// Only a repo that failed outright makes the exit non-zero.
func writeFleetReport(report *fleet.Report, stdout io.Writer, mdPath, jsonPath string) error {
	md := report.Markdown()
	if _, err := io.WriteString(stdout, md); err != nil {
		return err
	}
	if mdPath != "" {
		if err := os.WriteFile(mdPath, []byte(md), 0o644); err != nil {
			return fmt.Errorf("writing %s: %w", mdPath, err)
		}
	}
	if jsonPath != "" {
		data, err := report.JSON()
		if err != nil {
			return err
		}
		if err := os.WriteFile(jsonPath, data, 0o644); err != nil {
			return fmt.Errorf("writing %s: %w", jsonPath, err)
		}
	}
	if n := report.Failed(); n > 0 {
		return exitf(ExitUnresolved, "%d of %d repo(s) failed", n, len(report.Repos))
	}
	return nil
}

// fleetCheck runs what `check` (or, applying, `update`) runs, in process,
// on one repo: with its .doneram.pkl when it has one, and on what
// discovery finds when it does not. The commentary is dropped and the
// summary comes back through collect, since stdout belongs to the report.
func fleetCheck(ctx context.Context, dir string, apply bool) (*fleet.Result, error) {
	run := pklRun{dir: dir, apply: apply, workers: 8, log: io.Discard}
	cfg := filepath.Join(dir, doneramConfigName)
	if info, err := os.Stat(cfg); err == nil && !info.IsDir() {
		run.path = cfg
	}
	var summary *pklSummary
	run.collect = func(s pklSummary) { summary = &s }
	err := runCheckPkl(ctx, run)
	// A failed patch leaves a half-applied tree, which must not be
	// committed; anything else (a registry that did not answer for one
	// pin) is reported beside the results that stand.
	var exit *ExitError
	if summary == nil || (errors.As(err, &exit) && exit.Code == ExitPatch) {
		return nil, err
	}
	return fleetResult(*summary), err
}

// fleetResult reduces a repo's summary to the fleet report's counts and
// lines.
func fleetResult(s pklSummary) *fleet.Result {
	res := &fleet.Result{}
	c := &res.Counts
	type move struct{ tool, current, latest string }
	seen := map[move]bool{}
	for _, r := range s.Results {
		if r.EOL != nil && r.EOL.DaysLeft != nil && *r.EOL.DaysLeft <= eolWarnDays {
			c.EOL++
			if *r.EOL.DaysLeft < 0 {
				c.EOLPast++
			}
			res.Findings = append(res.Findings, fmt.Sprintf("end of life: %s %s %s (%s)", r.Tool, r.EOL.Product, r.EOL.Cycle, r.EOL.Date))
		}
		if len(r.Vulnerabilities) > 0 && (!r.Updated || !r.Security) {
			res.Findings = append(res.Findings, fmt.Sprintf("vulnerable: %s %s (%s)", r.Tool, r.Current, vulnIDs(r.Vulnerabilities)))
		}
		m := move{r.Tool, r.Current, r.Latest}
		if (!r.Updated && !r.needsUpdate) || seen[m] {
			continue
		}
		seen[m] = true
		line := fmt.Sprintf("%s %s -> %s", r.Tool, r.Current, r.Latest)
		if r.Security {
			line += " (security)"
		}
		res.Changes = append(res.Changes, line)
	}
	c.Updates = len(seen)
	c.Vulnerable = countVulnerable(s)
	for _, l := range s.Lockfiles {
		if !l.Updated {
			res.Findings = append(res.Findings, fmt.Sprintf("vulnerable: %s %s in %s (%s)", l.Package, l.Current, l.File, vulnIDs(l.Vulns)))
		} else {
			res.Changes = append(res.Changes, fmt.Sprintf("%s %s -> %s in %s (security)", l.Package, l.Current, l.Fixed, l.File))
		}
	}
	for _, t := range s.Templates {
		if t.Current != t.Latest {
			res.Changes = append(res.Changes, fmt.Sprintf("template %s %s -> %s", t.Template, t.Current, t.Latest))
		}
	}
	for _, h := range s.Hardened {
		// A refreshed digest is a pin moving, not a floating reference.
		if h.Kind != string(harden.Digest) {
			c.Floating++
		}
		if h.Updated {
			res.Changes = append(res.Changes, fmt.Sprintf("pin %s %s -> %s", h.Name, h.Ref, h.Pinned))
		}
	}
	for _, l := range s.Lifecycle {
		c.EOL++
		if l.Past {
			c.EOLPast++
		}
		res.Findings = append(res.Findings, fmt.Sprintf("end of life: %s %s %s (%s) in %s:%d", l.Name, l.Product, l.Cycle, l.Date, l.File, l.Line))
	}
	return res
}

func vulnIDs(vulns []pklVulnSummary) string {
	ids := make([]string, len(vulns))
	for i, v := range vulns {
		ids[i] = v.ID
	}
	return strings.Join(ids, ", ")
}
