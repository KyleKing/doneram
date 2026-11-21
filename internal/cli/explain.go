package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/urfave/cli/v3"

	"github.com/kyleking/doneram/internal/actionrisk"
	"github.com/kyleking/doneram/internal/changelog"
	"github.com/kyleking/doneram/internal/config"
	"github.com/kyleking/doneram/internal/discover"
	"github.com/kyleking/doneram/internal/engine"
	"github.com/kyleking/doneram/internal/httpclient"
	"github.com/kyleking/doneram/internal/locator"
	"github.com/kyleking/doneram/internal/provenance"
	"github.com/kyleking/doneram/internal/resolver"
)

// newExplainCommand prints the context needed to finish one bump on the
// current branch, for a person or a coding agent: the version change
// against the base branch, every place it is pinned, what the release
// notes say breaks and where this repo uses it, and the validate output.
// It reads; it never edits.
func newExplainCommand() *cli.Command {
	return &cli.Command{
		Name:      "explain",
		Usage:     "Print what finishing one tool's bump on this branch needs: the change, where it is pinned, breaking notes, and validate output",
		ArgsUsage: "<tool>",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:  "base",
				Usage: "the ref the bump is measured against (default: origin/HEAD, origin/main, main, or master, whichever exists)",
			},
			&cli.BoolFlag{
				Name:  "validate",
				Usage: "also run the config's validate command and include its output",
			},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			tool := cmd.Args().First()
			if tool == "" {
				return &ExitError{Code: ExitConfig, Err: errors.New("usage: doneram explain <tool>")}
			}
			return runExplain(ctx, os.Stdout, ".", tool, cmd.String("base"), cmd.Bool("validate"))
		},
	}
}

func runExplain(ctx context.Context, w io.Writer, dir, tool, base string, validate bool) error {
	cfg := &config.Config{}
	if path := filepath.Join(dir, ".doneram.pkl"); fileExists(path) {
		loaded, err := config.Load(path)
		if err != nil {
			return &ExitError{Code: ExitConfig, Err: fmt.Errorf("loading %s: %w", path, err)}
		}
		cfg = loaded
	}
	sites := cfg.Sites(dir)
	if cfg.DiscoverEnabled() {
		found, err := discover.Sites(dir)
		if err != nil {
			return err
		}
		if sites, err = discover.Merge(sites, found, cfg.Ignore); err != nil {
			return err
		}
	}
	sites, err := selectSites(sites, []string{tool})
	if err != nil {
		return &ExitError{Code: ExitConfig, Err: err}
	}

	ref, err := mergeBase(ctx, dir, base)
	if err != nil {
		return &ExitError{Code: ExitConfig, Err: err}
	}

	var results []engine.SiteResult
	var listing strings.Builder
	listed := map[string]bool{}
	var from, to string
	for _, s := range sites {
		matches, err := locator.Find(s.Locator)
		if err != nil || len(matches) == 0 {
			continue
		}
		old := valueAt(ctx, dir, ref.sha, s.Locator)
		if from == "" {
			from, to = old, matches[0].Value
		}
		// Risk and provenance read the move as on-disk value -> latest, so
		// the base value stands in for the pin and the branch's for latest.
		baseMatches := append([]locator.Match(nil), matches...)
		for i := range baseMatches {
			baseMatches[i].Value = old
		}
		results = append(results, engine.SiteResult{Site: s, Matches: baseMatches, Latest: matches[0].Value})
		// Two patterns can pin the same line (a URL and a package spec on
		// one import), so each line is listed once.
		for _, m := range matches {
			key := fmt.Sprintf("%s:%d", m.File, m.Line)
			if !listed[key] {
				listed[key] = true
				listing.WriteString(joinMatches(dir, []locator.Match{m}))
			}
		}
	}
	if len(results) == 0 {
		return &ExitError{Code: ExitMismatch, Err: fmt.Errorf("no site for %s matches anything on disk", tool)}
	}

	var b strings.Builder
	fmt.Fprintf(&b, "# %s\n\n", tool)
	switch from {
	case "":
		fmt.Fprintf(&b, "%s is new on this branch at `%s` (not pinned on `%s`).\n", tool, to, ref.name)
	case to:
		fmt.Fprintf(&b, "No change against `%s`: %s is `%s` on both. `doneram check --only %s` shows whether a newer release exists.\n", ref.name, tool, to, tool)
	default:
		fmt.Fprintf(&b, "This branch moves %s from `%s` to `%s` (against `%s`).\n", tool, from, to, ref.name)
		registry := resolver.Registry(httpclient.New(httpclient.DefaultConfig()))
		lookup := func(kind string) (resolver.Resolver, bool) { r, ok := registry[kind]; return r, ok }
		if link := tagLink(ctx, results[0].Site, from, to, lookup); link != "" {
			fmt.Fprintf(&b, "\nCompare: %s\n", link)
		}
	}

	fmt.Fprintf(&b, "\n## Where it is pinned\n\n")
	fmt.Fprint(&b, listing.String())

	if from != "" && from != to {
		client := httpclient.New(httpclient.DefaultConfig())
		risks := assessRisks(ctx, results, actionrisk.New(client))
		assessLibraryRisks(ctx, results, risks, changelog.New(client), dir)
		notes := checkProvenance(ctx, results, provenance.New(client))
		if r := risks[0]; r.Level != "" {
			fmt.Fprintf(&b, "\n## Breaking changes: %s\n\n", r.Level)
			if len(r.Notes) == 0 {
				fmt.Fprintf(&b, "The release notes name nothing this repo uses.\n")
			}
			for _, n := range r.Notes {
				fmt.Fprintf(&b, "- %s\n", n)
			}
		}
		if notes[0] != "" {
			fmt.Fprintf(&b, "\n## Provenance\n\n%s. Check that the new release really comes from the project before merging.\n", notes[0])
		}
	}

	fmt.Fprintf(&b, "\n## Validation\n\n")
	switch {
	case cfg.Validate == "":
		fmt.Fprintf(&b, "The config sets no `validate` command; use the repo's own test and lint commands.\n")
	case validate:
		ok, output := runValidate(ctx, cfg.Validate, dir)
		status := "passes"
		if !ok {
			status = "fails"
		}
		fmt.Fprintf(&b, "`%s` %s:\n\n```\n%s\n```\n", cfg.Validate, status, output)
	default:
		fmt.Fprintf(&b, "Run `%s` to check a fix (or pass --validate to include its output here).\n", cfg.Validate)
	}
	_, err = io.WriteString(w, b.String())
	return err
}

func joinMatches(dir string, matches []locator.Match) string {
	var b strings.Builder
	for _, m := range matches {
		fmt.Fprintf(&b, "- `%s:%d` `%s`\n", relTo(dir, m.File), m.Line, m.Value)
	}
	return b.String()
}

type baseRef struct{ name, sha string }

// mergeBase finds where this branch left its base, so the change shown is
// the branch's own and not whatever the base has gained since.
func mergeBase(ctx context.Context, dir, base string) (baseRef, error) {
	candidates := []string{base}
	if base == "" {
		candidates = []string{"origin/HEAD", "origin/main", "main", "origin/master", "master"}
	}
	for _, c := range candidates {
		if _, err := gitText(ctx, dir, "rev-parse", "--verify", "--quiet", c+"^{commit}"); err != nil {
			continue
		}
		sha, err := gitText(ctx, dir, "merge-base", "HEAD", c)
		if err != nil {
			return baseRef{}, fmt.Errorf("finding where HEAD left %s: %w", c, err)
		}
		return baseRef{name: c, sha: sha}, nil
	}
	if base != "" {
		return baseRef{}, fmt.Errorf("no ref %q", base)
	}
	return baseRef{}, errors.New("no base branch found; pass --base")
}

// valueAt is the site's first value in the files as they were at sha, or
// "" when the files or the pin did not exist there.
func valueAt(ctx context.Context, dir, sha string, l locator.Locator) string {
	files, err := filepath.Glob(l.Glob)
	if err != nil || len(files) == 0 {
		return ""
	}
	tmp, err := os.MkdirTemp("", "doneram-explain-")
	if err != nil {
		return ""
	}
	defer func() { _ = os.RemoveAll(tmp) }()
	top, err := gitText(ctx, dir, "rev-parse", "--show-toplevel")
	if err != nil {
		return ""
	}
	for _, f := range files {
		abs, _ := filepath.Abs(f)
		rel, err := filepath.Rel(top, abs)
		if err != nil {
			continue
		}
		content, err := gitText(ctx, dir, "show", sha+":"+filepath.ToSlash(rel))
		if err != nil {
			continue
		}
		copyPath := filepath.Join(tmp, "f")
		if err := os.WriteFile(copyPath, []byte(content+"\n"), 0o644); err != nil {
			continue
		}
		old := l
		old.Glob = copyPath
		old.Expect = 0
		if ms, err := locator.Find(old); err == nil && len(ms) > 0 {
			return ms[0].Value
		}
	}
	return ""
}

func gitText(ctx context.Context, dir string, args ...string) (string, error) {
	out, err := exec.CommandContext(ctx, "git", append([]string{"-C", dir}, args...)...).Output()
	return strings.TrimSpace(string(out)), err
}
