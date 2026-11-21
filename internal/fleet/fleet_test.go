package fleet

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func gitT(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-c", "user.email=t@example.com", "-c", "user.name=t", "-c", "commit.gpgsign=false"}, args...)...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v: %s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

// bareRemote makes a bare repo holding files on main, standing in for
// GitHub's side of a clone and push.
func bareRemote(t *testing.T, files map[string]string) string {
	t.Helper()
	work := t.TempDir()
	gitT(t, work, "init", "-q", "-b", "main")
	for name, body := range files {
		path := filepath.Join(work, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	gitT(t, work, "add", ".")
	gitT(t, work, "commit", "-qm", "init")
	remote := filepath.Join(t.TempDir(), "remote.git")
	gitT(t, work, "clone", "-q", "--bare", work, remote)
	return remote
}

// fakeGitHub serves the pull request endpoints a fleet run calls, keeping
// pull requests in memory.
type fakeGitHub struct {
	mu     sync.Mutex
	prs    []PR
	calls  []string
	closed []int
}

func (f *fakeGitHub) handler(t *testing.T) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		f.calls = append(f.calls, r.Method+" "+r.URL.Path)
		if r.Header.Get("Authorization") != "Bearer tok" {
			t.Errorf("%s %s: Authorization = %q", r.Method, r.URL.Path, r.Header.Get("Authorization"))
		}
		switch {
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/pulls"):
			head := r.URL.Query().Get("head")
			var out []PR
			for _, pr := range f.prs {
				if head == "" || "acme:"+pr.Head.Ref == head {
					out = append(out, pr)
				}
			}
			if out == nil {
				out = []PR{}
			}
			_ = json.NewEncoder(w).Encode(out)
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/pulls"):
			var req struct {
				Title, Body, Head, Base string
				Draft                   bool
			}
			_ = json.NewDecoder(r.Body).Decode(&req)
			if !req.Draft || req.Base != "main" {
				t.Errorf("create = %+v, want a draft into main", req)
			}
			pr := PR{Number: 100 + len(f.prs), Title: req.Title, Body: req.Body}
			pr.HTMLURL = fmt.Sprintf("https://github.test/acme/app/pull/%d", pr.Number)
			pr.Head.Ref = req.Head
			pr.Head.Repo = &struct {
				FullName string `json:"full_name"`
			}{"acme/app"}
			f.prs = append(f.prs, pr)
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(pr)
		case r.Method == http.MethodPatch && strings.Contains(r.URL.Path, "/pulls/"):
			var req map[string]string
			_ = json.NewDecoder(r.Body).Decode(&req)
			for i := range f.prs {
				if strings.HasSuffix(r.URL.Path, fmt.Sprintf("/pulls/%d", f.prs[i].Number)) {
					if req["state"] == "closed" {
						f.closed = append(f.closed, f.prs[i].Number)
						f.prs = append(f.prs[:i], f.prs[i+1:]...)
						w.WriteHeader(http.StatusOK)
						_, _ = io.WriteString(w, "{}")
						return
					}
					f.prs[i].Title, f.prs[i].Body = req["title"], req["body"]
					_ = json.NewEncoder(w).Encode(f.prs[i])
					return
				}
			}
			http.NotFound(w, r)
		case r.Method == http.MethodDelete:
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
	})
}

func (f *fakeGitHub) count(prefix string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, c := range f.calls {
		if strings.HasPrefix(c, prefix) {
			n++
		}
	}
	return n
}

// stubPlanner plans fixed groups; applying one writes its file.
type stubPlanner struct {
	groups map[string]string // group id -> deps.txt contents
	order  []string
	title  string
	plans  int
}

func (s *stubPlanner) Plan(_ context.Context, dir string, apply bool, _ io.Writer) (Plan, error) {
	s.plans++
	plan := Plan{Counts: Counts{Updates: len(s.order)}}
	for _, id := range s.order {
		plan.Groups = append(plan.Groups, Group{ID: id, Branch: "doneram/" + id, Title: s.title + " " + id, Body: "body " + id, Payload: id})
	}
	if apply {
		// A plan run patches the tree; ship must start each group clean.
		_ = os.WriteFile(filepath.Join(dir, "plan-leftover.txt"), []byte("x"), 0o644)
	}
	return plan, nil
}

func (s *stubPlanner) Apply(_ context.Context, dir string, g Group, _ io.Writer) error {
	if _, err := os.Stat(filepath.Join(dir, "plan-leftover.txt")); err == nil {
		return fmt.Errorf("group %s started on a dirty tree", g.ID)
	}
	file := "deps.txt"
	if strings.Contains(g.ID, "wf") {
		file = ".github/workflows/ci.yml"
	}
	return os.WriteFile(filepath.Join(dir, file), []byte(s.groups[g.ID]), 0o644)
}

func newRunner(t *testing.T, remote string, gh *fakeGitHub, planner Planner) *Runner {
	srv := httptest.NewServer(gh.handler(t))
	t.Cleanup(srv.Close)
	return &Runner{
		GitHub:   NewGitHub(srv.URL, "tok"),
		Git:      &Git{Token: "tok", Server: "https://github.test"},
		Planner:  planner,
		CloneURL: func(string) string { return "file://" + remote },
	}
}

func remoteFile(t *testing.T, remote, ref string) string {
	t.Helper()
	return gitT(t, remote, "show", ref+":deps.txt")
}

func TestRunOpensThenUpdatesPRs(t *testing.T) {
	remote := bareRemote(t, map[string]string{"deps.txt": "v1\n"})
	gh := &fakeGitHub{}
	planner := &stubPlanner{groups: map[string]string{"updates": "v2\n", "review-x": "v3\n"}, order: []string{"updates", "review-x"}, title: "chore:"}
	runner := newRunner(t, remote, gh, planner)
	repos := []Repo{{FullName: "acme/app", DefaultBranch: "main"}}

	report, err := runner.Run(context.Background(), repos, Options{Workers: 2})
	if err != nil {
		t.Fatal(err)
	}
	rep := report.Repos[0]
	if rep.Status != StatusOK || len(rep.Groups) != 2 {
		t.Fatalf("report = %+v", rep)
	}
	for _, g := range rep.Groups {
		if g.Action != ActionOpened || g.URL == "" {
			t.Errorf("group %s = %+v, want opened", g.ID, g)
		}
	}
	if got := remoteFile(t, remote, "doneram/updates"); got != "v2" {
		t.Errorf("doneram/updates deps.txt = %q", got)
	}
	if got := remoteFile(t, remote, "doneram/review-x"); got != "v3" {
		t.Errorf("doneram/review-x deps.txt = %q", got)
	}
	if got := remoteFile(t, remote, "main"); got != "v1" {
		t.Errorf("main deps.txt = %q, want untouched", got)
	}
	if got := gitT(t, remote, "log", "-1", "--format=%an <%ae>|%s", "doneram/updates"); got != DefaultAuthor+"|chore: updates" {
		t.Errorf("commit = %q", got)
	}

	// Same plan again: branches already hold these trees, so nothing is
	// pushed and the PRs are left as they are.
	before := gitT(t, remote, "rev-parse", "doneram/updates")
	report, _ = runner.Run(context.Background(), repos, Options{})
	for _, g := range report.Repos[0].Groups {
		if g.Action != ActionUnchanged {
			t.Errorf("rerun group %s = %+v, want unchanged", g.ID, g)
		}
	}
	if after := gitT(t, remote, "rev-parse", "doneram/updates"); after != before {
		t.Errorf("unchanged branch was pushed again: %s -> %s", before, after)
	}

	// A new release: the branch is force-pushed and the PR rewritten.
	planner.groups["updates"] = "v4\n"
	planner.title = "fix:"
	report, _ = runner.Run(context.Background(), repos, Options{})
	if g := report.Repos[0].Groups[0]; g.Action != ActionUpdated || g.Number != 100 {
		t.Errorf("updated group = %+v", g)
	}
	if got := remoteFile(t, remote, "doneram/updates"); got != "v4" {
		t.Errorf("doneram/updates deps.txt = %q", got)
	}
	if n := gh.count("POST"); n != 2 {
		t.Errorf("%d PRs created, want 2", n)
	}
	if gh.prs[0].Title != "fix: updates" {
		t.Errorf("PR title = %q", gh.prs[0].Title)
	}
}

func TestRunDryRunPushesNothing(t *testing.T) {
	remote := bareRemote(t, map[string]string{"deps.txt": "v1\n"})
	gh := &fakeGitHub{}
	planner := &stubPlanner{groups: map[string]string{"updates": "v2\n"}, order: []string{"updates"}, title: "chore:"}
	runner := newRunner(t, remote, gh, planner)

	report, err := runner.Run(context.Background(), []Repo{{FullName: "acme/app", DefaultBranch: "main"}}, Options{DryRun: true})
	if err != nil {
		t.Fatal(err)
	}
	if g := report.Repos[0].Groups; len(g) != 1 || g[0].Action != ActionPlanned {
		t.Errorf("groups = %+v", g)
	}
	if refs := gitT(t, remote, "for-each-ref", "--format=%(refname)"); refs != "refs/heads/main" {
		t.Errorf("remote refs = %q, want main alone", refs)
	}
	if len(gh.calls) != 0 {
		t.Errorf("dry run called the API: %v", gh.calls)
	}
	if md := report.Markdown(); !strings.Contains(md, "(dry run)") || !strings.Contains(md, "`updates`") {
		t.Errorf("markdown = %s", md)
	}
}

func TestRunWorkflowPushRejectedContinues(t *testing.T) {
	remote := bareRemote(t, map[string]string{"deps.txt": "v1\n", ".github/workflows/ci.yml": "on: push\n"})
	// GitHub refuses a workflow-file push from a token without the
	// workflows permission; a pre-receive hook says the same here.
	hook := filepath.Join(remote, "hooks", "pre-receive")
	script := "#!/bin/sh\nwhile read old new ref; do case \"$ref\" in *wf*) echo 'refusing to allow a GitHub App to create or update workflow `.github/workflows/ci.yml` without `workflows` permission' >&2; exit 1;; esac; done\n"
	if err := os.WriteFile(hook, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	gh := &fakeGitHub{}
	planner := &stubPlanner{groups: map[string]string{"review-wf": "on: pull_request\n", "updates": "v2\n"}, order: []string{"review-wf", "updates"}, title: "chore:"}
	runner := newRunner(t, remote, gh, planner)

	report, _ := runner.Run(context.Background(), []Repo{{FullName: "acme/app", DefaultBranch: "main"}}, Options{})
	rep := report.Repos[0]
	if rep.Status != StatusOK || report.Failed() != 0 {
		t.Errorf("a group failure failed the repo: %+v", rep)
	}
	if g := rep.Groups[0]; g.Action != ActionFailed || !strings.Contains(g.Error, "workflows permission") {
		t.Errorf("workflow group = %+v", g)
	}
	if g := rep.Groups[1]; g.Action != ActionOpened {
		t.Errorf("updates group = %+v, want it opened after the rejection", g)
	}
}

func TestRunCloseStale(t *testing.T) {
	remote := bareRemote(t, map[string]string{"deps.txt": "v1\n"})
	gh := &fakeGitHub{}
	stale := PR{Number: 7, HTMLURL: "https://github.test/acme/app/pull/7"}
	stale.Head.Ref = "doneram/review-gone"
	stale.Head.Repo = &struct {
		FullName string `json:"full_name"`
	}{"acme/app"}
	other := PR{Number: 8}
	other.Head.Ref = "feature/mine"
	other.Head.Repo = stale.Head.Repo
	gh.prs = []PR{stale, other}
	planner := &stubPlanner{groups: map[string]string{"updates": "v2\n"}, order: []string{"updates"}, title: "chore:"}
	runner := newRunner(t, remote, gh, planner)
	repos := []Repo{{FullName: "acme/app", DefaultBranch: "main"}}

	if _, err := runner.Run(context.Background(), repos, Options{}); err != nil {
		t.Fatal(err)
	}
	if len(gh.closed) != 0 {
		t.Fatalf("closed %v without --close-stale", gh.closed)
	}
	report, _ := runner.Run(context.Background(), repos, Options{CloseStale: true})
	if len(gh.closed) != 1 || gh.closed[0] != 7 {
		t.Errorf("closed = %v, want only #7", gh.closed)
	}
	if c := report.Repos[0].Closed; len(c) != 1 || c[0].Branch != "doneram/review-gone" {
		t.Errorf("report closed = %+v", c)
	}
	if gh.count("DELETE /repos/acme/app/git/refs/heads/doneram/review-gone") != 1 {
		t.Errorf("calls = %v", gh.calls)
	}
}

func TestRunCloneFailureFailsRepo(t *testing.T) {
	gh := &fakeGitHub{}
	runner := newRunner(t, filepath.Join(t.TempDir(), "missing.git"), gh, &stubPlanner{})
	report, err := runner.Run(context.Background(), []Repo{{FullName: "acme/gone"}}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if report.Failed() != 1 || !strings.Contains(report.Repos[0].Errors[0], "clone") {
		t.Errorf("report = %+v", report.Repos[0])
	}
}

func TestRunEmptyRepoSkipped(t *testing.T) {
	remote := filepath.Join(t.TempDir(), "empty.git")
	gitT(t, t.TempDir(), "init", "-q", "--bare", remote)
	runner := newRunner(t, remote, &fakeGitHub{}, &stubPlanner{})
	report, _ := runner.Run(context.Background(), []Repo{{FullName: "acme/empty"}}, Options{})
	if r := report.Repos[0]; r.Status != StatusSkipped {
		t.Errorf("report = %+v", r)
	}
}

func TestOwnedBranch(t *testing.T) {
	for branch, want := range map[string]bool{
		"doneram/updates":                 true,
		"doneram/review-actions-checkout": true,
		"main":                            false,
		"doneram/../main":                 false,
		"doneram/":                        false,
		"refs/heads/doneram/updates":      false,
	} {
		if got := ownedBranch(branch); got != want {
			t.Errorf("ownedBranch(%q) = %v", branch, got)
		}
	}
}

func TestGitRedactsToken(t *testing.T) {
	g := &Git{Token: "s3cr3t-token", Server: "https://github.test"}
	_, err := g.Run(context.Background(), t.TempDir(), "clone", "--quiet", "file:///nonexistent/s3cr3t-token.git", "x")
	if err == nil || strings.Contains(err.Error(), "s3cr3t-token") || !strings.Contains(err.Error(), "***") {
		t.Errorf("err = %v", err)
	}
}

func TestTruncateBody(t *testing.T) {
	long := strings.Repeat("| row |\n", maxBody/4)
	got := truncateBody(long)
	if len(got) > 65536 || !strings.Contains(got, "Truncated") || strings.Contains(got[:len(got)-60], "| row\n") {
		t.Errorf("truncated to %d bytes", len(got))
	}
	if truncateBody("short") != "short" {
		t.Error("short body changed")
	}
}
