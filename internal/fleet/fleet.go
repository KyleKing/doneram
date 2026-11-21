// Package fleet runs doneram across the git repositories under one local
// directory. By default it only reports what each repo needs. With apply
// it commits each repo's updates to the Branch branch and leaves the
// checked-out branch and working tree as they were. It never pushes.
//
// The run itself lives with the CLI, which hands it in as a Check.
package fleet

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

// Branch is where an apply run commits each repo's updates.
const Branch = "doneram/updates"

// Counts is what a run found in one repo, for the fleet report.
type Counts struct {
	// Updates counts distinct pin moves.
	Updates int `json:"updates"`
	// Vulnerable counts pins and lockfile packages with a known advisory
	// still on disk.
	Vulnerable int `json:"vulnerable"`
	// Floating counts actions used by tag, mise prefixes, and image tags
	// without a digest.
	Floating int `json:"floating"`
	// EOL counts references past or near end of life; EOLPast, the ones
	// already past it.
	EOL     int `json:"eol"`
	EOLPast int `json:"eolPast"`
}

// Result is one repo's doneram run.
type Result struct {
	Counts Counts
	// Changes has one line per pin that is behind, or that moved when the
	// run applied; they become the commit body.
	Changes []string
	// Findings has one line per advisory or end-of-life item.
	Findings []string
}

// Check runs doneram in dir, patching the tree when apply is set. A nil
// Result means the run did not finish and its changes must not stand; a
// Result with an error finished, with a problem worth reporting.
type Check func(ctx context.Context, dir string, apply bool) (*Result, error)

// Repos returns each directory directly under dir whose name matches
// match and not exclude (nil excludes nothing) and that is a git
// repository, in name order.
func Repos(dir string, match, exclude *regexp.Regexp) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var repos []string
	for _, e := range entries {
		name := e.Name()
		if !e.IsDir() || !match.MatchString(name) || (exclude != nil && exclude.MatchString(name)) {
			continue
		}
		path := filepath.Join(dir, name)
		if _, err := os.Stat(filepath.Join(path, ".git")); err == nil {
			repos = append(repos, path)
		}
	}
	return repos, nil
}

// Options are a fleet run's choices.
type Options struct {
	Apply   bool
	Workers int
	// Log receives one progress line per repo; nil discards them.
	Log io.Writer
}

// Run covers every repo, opts.Workers at a time, and returns one report in
// the order the repos were given.
func Run(ctx context.Context, repos []string, opts Options, check Check) *Report {
	report := &Report{Apply: opts.Apply, Started: time.Now().UTC(), Repos: make([]RepoReport, len(repos))}
	var mu sync.Mutex
	jobs := make(chan int)
	var wg sync.WaitGroup
	for range max(opts.Workers, 1) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range jobs {
				rep := runRepo(ctx, repos[i], opts.Apply, check)
				report.Repos[i] = rep
				if opts.Log != nil {
					mu.Lock()
					_, _ = fmt.Fprintf(opts.Log, "%s: %s\n", rep.Repo, statusText(rep))
					mu.Unlock()
				}
			}
		}()
	}
	for i := range repos {
		jobs <- i
	}
	close(jobs)
	wg.Wait()
	report.Finished = time.Now().UTC()
	return report
}

func runRepo(ctx context.Context, dir string, apply bool, check Check) RepoReport {
	rep := RepoReport{Repo: filepath.Base(dir), Path: dir, Status: StatusOK}
	if !apply {
		res, err := check(ctx, dir, false)
		rep.record(res, err)
		return rep
	}
	applyRepo(ctx, dir, check, &rep)
	return rep
}

func (rep *RepoReport) record(res *Result, err error) {
	if res != nil {
		rep.Counts, rep.Changes, rep.Findings = res.Counts, res.Changes, res.Findings
	} else {
		rep.Status = StatusFailed
		if err == nil {
			err = errors.New("the run did not finish")
		}
	}
	if err != nil {
		rep.Errors = append(rep.Errors, err.Error())
	}
}

func (rep *RepoReport) fail(err error) {
	rep.Status = StatusFailed
	rep.Errors = append(rep.Errors, err.Error())
}

// applyRepo runs the update on a detached HEAD, commits what changed, and
// points Branch at that commit. The branch the repo had checked out is
// never written to, and is checked out again afterwards.
func applyRepo(ctx context.Context, dir string, check Check, rep *RepoReport) {
	skip := func(why string) { rep.Status, rep.Skipped = StatusSkipped, why }
	status, err := git(ctx, dir, "status", "--porcelain")
	if err != nil {
		rep.fail(err)
		return
	}
	if status != "" {
		skip("uncommitted changes")
		return
	}
	head, err := git(ctx, dir, "rev-parse", "--verify", "--quiet", "HEAD")
	if err != nil {
		skip("no commits")
		return
	}
	back := []string{"switch", "--quiet", "--detach", head}
	if branch, err := git(ctx, dir, "symbolic-ref", "--quiet", "--short", "HEAD"); err == nil {
		if branch == Branch {
			skip(Branch + " is checked out")
			return
		}
		back = []string{"switch", "--quiet", branch}
	}
	if _, err := git(ctx, dir, "switch", "--quiet", "--detach"); err != nil {
		rep.fail(err)
		return
	}
	defer func() {
		// Whatever the run left uncommitted goes: the tree was clean.
		for _, args := range [][]string{{"reset", "--quiet", "--hard"}, {"clean", "-fdq"}, back} {
			if _, err := git(ctx, dir, args...); err != nil {
				rep.fail(err)
				return
			}
		}
	}()

	res, err := check(ctx, dir, true)
	rep.record(res, err)
	if res == nil {
		return
	}
	if changed, err := git(ctx, dir, "status", "--porcelain"); err != nil || changed == "" {
		if err != nil {
			rep.fail(err)
		}
		return
	}
	msg := "chore: doneram updates"
	if len(res.Changes) > 0 {
		msg += "\n\n- " + strings.Join(res.Changes, "\n- ")
	}
	sha := ""
	for _, args := range [][]string{
		{"add", "--all"},
		{"commit", "--quiet", "--no-verify", "-m", msg},
		{"rev-parse", "HEAD"},
	} {
		if sha, err = git(ctx, dir, args...); err != nil {
			rep.fail(err)
			return
		}
	}
	if _, err := git(ctx, dir, "branch", "--force", Branch, sha); err != nil {
		rep.fail(err)
		return
	}
	rep.Branch, rep.Commit = Branch, sha
}

// git runs git in dir and returns its trimmed stdout.
func git(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	out, err := cmd.Output()
	if err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			err = fmt.Errorf("%w: %s", err, strings.TrimSpace(string(exit.Stderr)))
		}
		return "", fmt.Errorf("git %s: %w", args[0], err)
	}
	return strings.TrimSpace(string(out)), nil
}
