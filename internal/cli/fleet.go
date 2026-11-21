package cli

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/urfave/cli/v3"

	"github.com/kyleking/doneram/internal/fleet"
	"github.com/kyleking/doneram/internal/harden"
	"github.com/kyleking/doneram/internal/resolver"
)

func newFleetCommand() *cli.Command {
	return &cli.Command{
		Name:  "fleet",
		Usage: "Run doneram across many repos: one report, and each repo's grouped pull requests opened from here",
		Flags: []cli.Flag{
			&cli.StringSliceFlag{
				Name:  "repo",
				Usage: "a repo to cover, as owner/name (repeatable)",
			},
			&cli.StringFlag{
				Name:  "repos-file",
				Usage: "a file listing repos to cover, one owner/name per line; # starts a comment",
			},
			&cli.StringFlag{
				Name:  "org",
				Usage: "cover every repo this organization or user owns, except archived repos and forks",
			},
			&cli.StringSliceFlag{
				Name:  "exclude",
				Usage: "leave out this repo, as owner/name or name (repeatable)",
			},
			&cli.BoolFlag{
				Name:  "dry-run",
				Usage: "plan and report only: nothing is patched, pushed, or opened",
			},
			&cli.BoolFlag{
				Name:  "close-stale",
				Usage: "close open doneram/* pull requests whose group the plan no longer has, and delete their branches",
			},
			&cli.IntFlag{
				Name:  "workers",
				Usage: "number of repos covered in parallel",
				Value: 4,
			},
			&cli.StringFlag{
				Name:  "workdir",
				Usage: "clone into this directory and keep the clones and each repo's run log; default is a temporary directory",
			},
			&cli.StringFlag{
				Name:  "author",
				Usage: "commit author, as \"Name <email>\"",
				Value: fleet.DefaultAuthor,
			},
			&cli.StringFlag{
				Name:  "report",
				Usage: "also write the markdown report to this path",
			},
			&cli.StringFlag{
				Name:  "json",
				Usage: "write the report as JSON to this path",
			},
			&cli.DurationFlag{
				Name:  "min-age",
				Usage: "hold back a release until it is this old; 0 disables",
				Value: resolver.DefaultCooldown,
			},
		},
		Action: runFleet,
	}
}

func runFleet(ctx context.Context, cmd *cli.Command) error {
	ctx = resolver.ContextWithCooldown(ctx, cmd.Duration("min-age"))
	token := os.Getenv("GITHUB_TOKEN")
	// Each covered repo's validate, afterPatch, command tools, and package
	// managers run in this process's environment, so the owner-wide write
	// token must not be in it: one repo's code could otherwise push to
	// every other. Only this process's GitHub client and git calls hold
	// it. Resolvers still want a token for rate limits; DONERAM_READ_TOKEN,
	// when set, takes GITHUB_TOKEN's place for them and for that code.
	_ = os.Unsetenv("GITHUB_TOKEN")
	_ = os.Unsetenv("GH_TOKEN")
	if read := os.Getenv("DONERAM_READ_TOKEN"); read != "" {
		_ = os.Setenv("GITHUB_TOKEN", read)
	}
	_ = os.Unsetenv("DONERAM_READ_TOKEN")
	dryRun := cmd.Bool("dry-run")
	if token == "" && !dryRun {
		return exitf(ExitConfig, "fleet pushes branches and opens pull requests, so it needs GITHUB_TOKEN; --dry-run works without one for public repos")
	}

	gh := fleet.NewGitHub(os.Getenv("GITHUB_API_URL"), token)
	server := os.Getenv("GITHUB_SERVER_URL")
	if server == "" {
		server = "https://github.com"
	}
	server = strings.TrimRight(server, "/")

	repos, err := fleetRepos(ctx, gh, cmd.StringSlice("repo"), cmd.String("repos-file"), cmd.String("org"), cmd.StringSlice("exclude"))
	if err != nil {
		return err
	}
	if len(repos) == 0 {
		return exitf(ExitConfig, "no repos to cover: pass --repo, --repos-file, or --org")
	}

	runner := &fleet.Runner{
		GitHub:   gh,
		Git:      &fleet.Git{Token: token, Server: server},
		Planner:  fleetPlanner{workers: 8},
		CloneURL: func(full string) string { return server + "/" + full + ".git" },
		Log:      os.Stderr,
	}
	report, err := runner.Run(ctx, repos, fleet.Options{
		DryRun:     dryRun,
		CloseStale: cmd.Bool("close-stale"),
		Workers:    int(cmd.Int("workers")),
		Workdir:    cmd.String("workdir"),
		Author:     cmd.String("author"),
	})
	if err != nil {
		return &ExitError{Code: ExitConfig, Err: err}
	}
	return writeFleetReport(report, os.Stdout, cmd.String("report"), cmd.String("json"))
}

// writeFleetReport prints the markdown, writes the files asked for, and
// appends to the job summary when running in Actions. Only a repo that
// failed outright makes the exit non-zero; a failed group is in the
// report, beside the groups that went through.
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
	if summary := os.Getenv("GITHUB_STEP_SUMMARY"); summary != "" {
		f, err := os.OpenFile(summary, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
		if err != nil {
			return fmt.Errorf("opening GITHUB_STEP_SUMMARY: %w", err)
		}
		_, werr := io.WriteString(f, md+"\n")
		if cerr := f.Close(); werr == nil {
			werr = cerr
		}
		if werr != nil {
			return fmt.Errorf("writing GITHUB_STEP_SUMMARY: %w", werr)
		}
	}
	if n := report.Failed(); n > 0 {
		return exitf(ExitUnresolved, "%d of %d repo(s) failed", n, len(report.Repos))
	}
	return nil
}

var repoName = regexp.MustCompile(`^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$`)

// fleetRepos gathers the repos to cover from the flags. A repo named
// outright is covered even when archived or a fork, since someone asked
// for it; an org listing leaves those out.
func fleetRepos(ctx context.Context, gh *fleet.GitHub, named []string, file, org string, exclude []string) ([]fleet.Repo, error) {
	if file != "" {
		listed, err := readReposFile(file)
		if err != nil {
			return nil, &ExitError{Code: ExitConfig, Err: err}
		}
		named = append(named, listed...)
	}

	var repos []fleet.Repo
	for _, name := range named {
		name = strings.TrimSpace(name)
		if !repoName.MatchString(name) {
			return nil, exitf(ExitConfig, "repo %q is not owner/name", name)
		}
		r, err := gh.Repo(ctx, name)
		if err != nil {
			// The clone still decides: without a readable API answer the
			// default branch comes from the clone's HEAD.
			r = fleet.Repo{FullName: name}
		}
		repos = append(repos, r)
	}
	if org != "" {
		listed, err := gh.OwnerRepos(ctx, org)
		if err != nil {
			return nil, exitf(ExitUnresolved, "listing %s's repos: %v", org, err)
		}
		sort.Slice(listed, func(i, j int) bool { return strings.ToLower(listed[i].FullName) < strings.ToLower(listed[j].FullName) })
		for _, r := range listed {
			if !r.Archived && !r.Fork {
				repos = append(repos, r)
			}
		}
	}

	skip := map[string]bool{}
	for _, e := range exclude {
		skip[strings.ToLower(strings.TrimSpace(e))] = true
	}
	seen := map[string]bool{}
	kept := repos[:0]
	for _, r := range repos {
		key := strings.ToLower(r.FullName)
		_, short, _ := strings.Cut(key, "/")
		if seen[key] || skip[key] || skip[short] {
			continue
		}
		seen[key] = true
		kept = append(kept, r)
	}
	return kept, nil
}

func readReposFile(path string) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	var names []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line, _, _ := strings.Cut(scanner.Text(), "#")
		if line = strings.TrimSpace(line); line != "" {
			names = append(names, line)
		}
	}
	return names, scanner.Err()
}

// fleetPlanner runs the same plan and group runs `update` does, in
// process, on a clone: with the clone's .doneram.pkl when it has one, and
// on what discovery finds when it does not. The summary comes back through
// collect instead of stdout and $GITHUB_OUTPUT, which belong to one repo.
type fleetPlanner struct{ workers int }

func (p fleetPlanner) pklRun(dir string, log io.Writer) pklRun {
	run := pklRun{dir: dir, workers: p.workers, log: log}
	cfg := filepath.Join(dir, doneramConfigName)
	if info, err := os.Stat(cfg); err == nil && !info.IsDir() {
		run.path = cfg
	}
	return run
}

func (p fleetPlanner) Plan(ctx context.Context, dir string, apply bool, log io.Writer) (fleet.Plan, error) {
	run := p.pklRun(dir, log)
	run.apply = apply
	var summary *pklSummary
	run.collect = func(s pklSummary) { summary = &s }
	err := runCheckPkl(ctx, run)
	if summary == nil {
		if err == nil {
			err = errors.New("the run produced no summary")
		}
		return fleet.Plan{}, err
	}
	plan := fleet.Plan{Counts: fleetCounts(*summary)}
	for _, g := range summary.Groups {
		plan.Groups = append(plan.Groups, fleet.Group{ID: g.ID, Branch: g.Branch, Title: g.Title, Body: g.Body, Payload: g})
	}
	if err != nil {
		return plan, &fleet.PartialError{Err: err}
	}
	return plan, nil
}

// Apply runs one group. A patch that failed leaves a half-applied tree, so
// it fails the group; anything else (a registry that did not answer for an
// unrelated pin) is reported and the group's changes still ship.
func (p fleetPlanner) Apply(ctx context.Context, dir string, g fleet.Group, log io.Writer) error {
	group, ok := g.Payload.(pklGroup)
	if !ok {
		return fmt.Errorf("group %s carries no plan", g.ID)
	}
	run := p.pklRun(dir, log)
	run.apply = true
	run.group = &group
	finished := false
	run.collect = func(pklSummary) { finished = true }
	err := runCheckPkl(ctx, run)
	if err == nil {
		return nil
	}
	var exit *ExitError
	if !finished || (errors.As(err, &exit) && exit.Code == ExitPatch) {
		return err
	}
	return &fleet.PartialError{Err: err}
}

// fleetCounts reduces a repo's summary to the fleet report's columns.
func fleetCounts(s pklSummary) fleet.Counts {
	var c fleet.Counts
	type move struct{ tool, current, latest string }
	seen := map[move]bool{}
	security := map[move]bool{}
	for _, r := range s.Results {
		if r.EOL != nil && r.EOL.DaysLeft != nil && *r.EOL.DaysLeft <= eolWarnDays {
			c.EOL++
			if *r.EOL.DaysLeft < 0 {
				c.EOLPast++
			}
		}
		if !r.Updated && !r.needsUpdate {
			continue
		}
		m := move{r.Tool, r.Current, r.Latest}
		seen[m] = true
		if r.Security {
			security[m] = true
		}
	}
	c.Updates, c.Security = len(seen), len(security)
	c.LockAdvisories = len(s.Lockfiles)
	for _, h := range s.Hardened {
		// A refreshed digest is a pin moving, not a floating reference.
		if h.Kind != string(harden.Digest) {
			c.Floating++
		}
	}
	for _, l := range s.Lifecycle {
		c.EOL++
		if l.Past {
			c.EOLPast++
		}
	}
	return c
}
