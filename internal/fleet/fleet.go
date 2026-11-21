// Package fleet runs doneram across many repositories from one process:
// it clones each, plans its updates, and either reports them (a dry run)
// or pushes each group to its doneram/<group> branch and opens or updates
// that group's pull request. The repos need no workflow of their own.
//
// The plan itself is not here. It lives with the CLI, which hands it in as
// a Planner, so this package owns only the clone, branch, and pull request
// plumbing around it.
package fleet

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

// Group is one pull request's worth of a repo's changes, as a plan run
// splits them. Payload is the planner's own record of the group, handed
// back to Apply untouched.
type Group struct {
	ID      string
	Branch  string
	Title   string
	Body    string
	Payload any
}

// Counts is what a plan found in one repo, for the fleet report.
type Counts struct {
	// Updates counts distinct pin moves; Security, the ones that clear a
	// published advisory.
	Updates  int `json:"updates"`
	Security int `json:"security"`
	// LockAdvisories counts vulnerable packages lockfiles resolve.
	LockAdvisories int `json:"lockAdvisories"`
	// Floating counts actions used by tag, mise prefixes, and image tags
	// without a digest.
	Floating int `json:"floating"`
	// EOL counts references past or near end of life; EOLPast, the ones
	// already past it.
	EOL     int `json:"eol"`
	EOLPast int `json:"eolPast"`
}

// Plan is one repo's plan run.
type Plan struct {
	Groups []Group
	Counts Counts
}

// Planner runs doneram itself on a clone. Plan with apply set patches
// the tree (validation and bisection need the patched tree), and Apply
// patches only one group's changes into a clean one. log receives the
// run's commentary.
type Planner interface {
	Plan(ctx context.Context, dir string, apply bool, log io.Writer) (Plan, error)
	Apply(ctx context.Context, dir string, g Group, log io.Writer) error
}

// PartialError is a run that finished, with its results standing, but hit
// something along the way: a registry that did not answer for one pin. It
// is reported without failing the repo, which is how one flaky lookup
// keeps from holding back every other pull request.
type PartialError struct{ Err error }

func (e *PartialError) Error() string { return e.Err.Error() }
func (e *PartialError) Unwrap() error { return e.Err }

// Options are a fleet run's choices.
type Options struct {
	// DryRun plans without patching and reports what each repo would get;
	// nothing is pushed and no pull request is touched.
	DryRun bool
	// CloseStale closes open doneram/* pull requests whose group this
	// run's plan no longer has, and deletes their branches.
	CloseStale bool
	Workers    int
	// Workdir keeps the clones (and each repo's run log beside its clone)
	// there; empty clones into a temporary directory removed afterwards.
	Workdir string
	// Author is the commit author, "Name <email>".
	Author string
}

// DefaultAuthor signs fleet commits unless --author says otherwise.
const DefaultAuthor = "doneram <doneram@users.noreply.github.com>"

// Runner carries what a fleet run talks to.
type Runner struct {
	GitHub  *GitHub
	Git     *Git
	Planner Planner
	// CloneURL maps owner/name to what git clones, so a test can serve a
	// local bare repository.
	CloneURL func(fullName string) string
	// Log receives one progress line per step; nil discards them.
	Log io.Writer
	mu  sync.Mutex
}

func (r *Runner) logf(repo, format string, a ...any) {
	if r.Log == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	_, _ = fmt.Fprintf(r.Log, "%s: %s\n", repo, fmt.Sprintf(format, a...))
}

var authorRe = regexp.MustCompile(`^\s*([^<>]+?)\s*<([^<>\s]+)>\s*$`)

func parseAuthor(s string) (name, email string, err error) {
	if s == "" {
		s = DefaultAuthor
	}
	m := authorRe.FindStringSubmatch(s)
	if m == nil {
		return "", "", fmt.Errorf("author %q is not \"Name <email>\"", s)
	}
	return m[1], m[2], nil
}

// Run covers every repo, opts.Workers at a time, and returns one report
// in the order the repos were given.
func (r *Runner) Run(ctx context.Context, repos []Repo, opts Options) (*Report, error) {
	name, email, err := parseAuthor(opts.Author)
	if err != nil {
		return nil, err
	}
	root := opts.Workdir
	if root == "" {
		if root, err = os.MkdirTemp("", "doneram-fleet-"); err != nil {
			return nil, err
		}
		defer func() { _ = os.RemoveAll(root) }()
	} else if root, err = filepath.Abs(root); err != nil {
		return nil, err
	}

	workers := opts.Workers
	if workers < 1 {
		workers = 1
	}
	report := &Report{DryRun: opts.DryRun, Started: time.Now().UTC(), Repos: make([]RepoReport, len(repos))}
	jobs := make(chan int)
	var wg sync.WaitGroup
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range jobs {
				job := repoJob{Runner: r, opts: opts, root: root, repo: repos[i], name: name, email: email}
				report.Repos[i] = job.run(ctx)
			}
		}()
	}
	for i := range repos {
		jobs <- i
	}
	close(jobs)
	wg.Wait()
	report.Finished = time.Now().UTC()
	return report, nil
}

type repoJob struct {
	*Runner
	opts        Options
	root        string
	repo        Repo
	name, email string
	dir, base   string
	log         io.Writer
}

func (j *repoJob) run(ctx context.Context) RepoReport {
	full := j.repo.FullName
	rep := RepoReport{Repo: full, Status: StatusOK}
	fail := func(format string, a ...any) RepoReport {
		rep.Status = StatusFailed
		rep.Errors = append(rep.Errors, fmt.Sprintf(format, a...))
		j.logf(full, "failed: %s", rep.Errors[len(rep.Errors)-1])
		return rep
	}

	j.dir = filepath.Join(j.root, filepath.FromSlash(full))
	if err := os.RemoveAll(j.dir); err != nil {
		return fail("clearing %s: %v", j.dir, err)
	}
	if err := os.MkdirAll(filepath.Dir(j.dir), 0o755); err != nil {
		return fail("%v", err)
	}
	logFile, err := os.Create(j.dir + ".log")
	if err != nil {
		return fail("%v", err)
	}
	defer func() { _ = logFile.Close() }()
	j.log = logFile

	j.logf(full, "cloning")
	if _, err := j.Git.Run(ctx, filepath.Dir(j.dir), "clone", "--depth", "1", "--no-tags", "--quiet", j.CloneURL(full), j.dir); err != nil {
		return fail("clone: %v", err)
	}
	base, err := j.Git.Run(ctx, j.dir, "rev-parse", "--verify", "--quiet", "HEAD")
	if err != nil || base == "" {
		rep.Status = StatusSkipped
		rep.Skipped = "empty repository"
		j.logf(full, "skipped: empty repository")
		return rep
	}
	j.base = base
	baseBranch := j.repo.DefaultBranch
	if baseBranch == "" {
		if baseBranch, err = j.Git.Run(ctx, j.dir, "symbolic-ref", "--short", "HEAD"); err != nil {
			return fail("reading the default branch: %v", err)
		}
	}

	j.logf(full, "planning")
	plan, err := j.Planner.Plan(ctx, j.dir, !j.opts.DryRun, j.log)
	if err != nil {
		var partial *PartialError
		if !errors.As(err, &partial) {
			return fail("plan: %v", err)
		}
		rep.Warnings = append(rep.Warnings, "plan: "+partial.Error())
	}
	rep.Counts = plan.Counts
	j.logf(full, "%d group(s)", len(plan.Groups))

	if j.opts.DryRun {
		for _, g := range plan.Groups {
			rep.Groups = append(rep.Groups, GroupReport{ID: g.ID, Branch: g.Branch, Title: g.Title, Action: ActionPlanned})
		}
		return rep
	}

	planned := map[string]bool{}
	for _, g := range plan.Groups {
		planned[g.Branch] = true
		gr := j.ship(ctx, g, baseBranch)
		if gr.Error != "" {
			j.logf(full, "%s: %s", g.ID, gr.Error)
		} else {
			j.logf(full, "%s: %s %s", g.ID, gr.Action, gr.URL)
		}
		rep.Groups = append(rep.Groups, gr)
	}
	if j.opts.CloseStale {
		j.closeStale(ctx, planned, &rep)
	}
	return rep
}

// ship applies one group to a clean tree, pushes it to the group's branch,
// and opens or updates the group's pull request.
func (j *repoJob) ship(ctx context.Context, g Group, baseBranch string) GroupReport {
	full := j.repo.FullName
	gr := GroupReport{ID: g.ID, Branch: g.Branch, Title: g.Title}
	failed := func(format string, a ...any) GroupReport {
		gr.Action = ActionFailed
		gr.Error = fmt.Sprintf(format, a...)
		return gr
	}
	// doneram owns doneram/* branches and force-pushes them every run;
	// anything else is never pushed, whatever a plan names.
	if !ownedBranch(g.Branch) {
		return failed("refusing to push %q: not a doneram/ branch", g.Branch)
	}

	if _, err := j.Git.Run(ctx, j.dir, "reset", "--quiet", "--hard", j.base); err != nil {
		return failed("%v", err)
	}
	if _, err := j.Git.Run(ctx, j.dir, "clean", "-fdq"); err != nil {
		return failed("%v", err)
	}
	if err := j.Planner.Apply(ctx, j.dir, g, j.log); err != nil {
		var partial *PartialError
		if !errors.As(err, &partial) {
			return failed("apply: %v", err)
		}
		gr.Warning = partial.Error()
	}
	status, err := j.Git.Run(ctx, j.dir, "status", "--porcelain")
	if err != nil {
		return failed("%v", err)
	}
	if status == "" {
		gr.Action = ActionNoChanges
		return gr
	}
	if _, err := j.Git.Run(ctx, j.dir, "add", "--all"); err != nil {
		return failed("%v", err)
	}
	if _, err := j.Git.Run(ctx, j.dir,
		"-c", "user.name="+j.name, "-c", "user.email="+j.email, "-c", "commit.gpgsign=false",
		"commit", "--quiet", "--no-verify", "-m", g.Title); err != nil {
		return failed("%v", err)
	}

	// A branch already holding exactly this tree is left alone, so an
	// unchanged group does not rerun CI or notify anyone.
	remote := "refs/remotes/origin/" + g.Branch
	pushed := true
	if _, err := j.Git.Run(ctx, j.dir, "fetch", "--quiet", "--depth", "1", "--no-tags", "origin", "+refs/heads/"+g.Branch+":"+remote); err == nil {
		ours, _ := j.Git.Run(ctx, j.dir, "rev-parse", "HEAD^{tree}")
		theirs, _ := j.Git.Run(ctx, j.dir, "rev-parse", remote+"^{tree}")
		pushed = ours == "" || ours != theirs
	}
	if pushed {
		if _, err := j.Git.Run(ctx, j.dir, "push", "--quiet", "--force", "origin", "HEAD:refs/heads/"+g.Branch); err != nil {
			msg := err.Error()
			if strings.Contains(msg, "workflow") {
				msg = "push rejected: changing .github/workflows needs a token with the workflows permission (" + msg + ")"
			}
			return failed("%s", msg)
		}
	}

	body := truncateBody(g.Body)
	pr, err := j.GitHub.OpenPR(ctx, full, g.Branch)
	if err != nil {
		return failed("finding the open pull request: %v", err)
	}
	switch {
	case pr == nil:
		created, err := j.GitHub.CreatePR(ctx, full, baseBranch, g.Branch, g.Title, body, true)
		if err != nil {
			return failed("opening the pull request: %v", err)
		}
		gr.Action, gr.Number, gr.URL = ActionOpened, created.Number, created.HTMLURL
	case !pushed && pr.Title == g.Title && pr.Body == body:
		gr.Action, gr.Number, gr.URL = ActionUnchanged, pr.Number, pr.HTMLURL
	default:
		updated, err := j.GitHub.UpdatePR(ctx, full, pr.Number, g.Title, body)
		if err != nil {
			return failed("updating pull request #%d: %v", pr.Number, err)
		}
		gr.Action, gr.Number, gr.URL = ActionUpdated, updated.Number, updated.HTMLURL
	}
	return gr
}

// closeStale closes each open doneram/* pull request whose group the plan
// no longer has: what it proposed has merged some other way, or moved on.
func (j *repoJob) closeStale(ctx context.Context, planned map[string]bool, rep *RepoReport) {
	full := j.repo.FullName
	prs, err := j.GitHub.OpenPRs(ctx, full)
	if err != nil {
		rep.Errors = append(rep.Errors, fmt.Sprintf("listing pull requests: %v", err))
		return
	}
	for _, pr := range prs {
		if pr.Head.Repo == nil || !strings.EqualFold(pr.Head.Repo.FullName, full) || !ownedBranch(pr.Head.Ref) || planned[pr.Head.Ref] {
			continue
		}
		if err := j.GitHub.ClosePR(ctx, full, pr.Number); err != nil {
			rep.Errors = append(rep.Errors, fmt.Sprintf("closing #%d: %v", pr.Number, err))
			continue
		}
		if err := j.GitHub.DeleteBranch(ctx, full, pr.Head.Ref); err != nil {
			rep.Warnings = append(rep.Warnings, fmt.Sprintf("closed #%d but kept %s: %v", pr.Number, pr.Head.Ref, err))
		}
		j.logf(full, "closed stale #%d (%s)", pr.Number, pr.Head.Ref)
		rep.Closed = append(rep.Closed, ClosedPR{Number: pr.Number, Branch: pr.Head.Ref, URL: pr.HTMLURL})
	}
}

var ownedRe = regexp.MustCompile(`^doneram/[a-z0-9][a-z0-9-]*$`)

func ownedBranch(branch string) bool { return ownedRe.MatchString(branch) }

// maxBody stays under GitHub's 65536-character limit on a pull request
// body, with room for the note saying it was cut.
const maxBody = 60000

func truncateBody(body string) string {
	if len(body) <= maxBody {
		return body
	}
	cut := body[:maxBody]
	// Back up to a rune boundary and a line end, so a table row or a
	// multi-byte character is not split.
	if i := strings.LastIndexByte(cut, '\n'); i > 0 {
		cut = cut[:i]
	}
	return cut + "\n\n*Truncated: the full list is in the fleet report.*\n"
}
