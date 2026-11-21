package fleet

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func gitT(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v: %s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

// newRepo makes a git repo at parent/name with one commit on main holding
// deps.txt.
func newRepo(t *testing.T, parent, name string) string {
	t.Helper()
	dir := filepath.Join(parent, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	gitT(t, dir, "init", "-q", "-b", "main")
	gitT(t, dir, "config", "user.email", "t@example.com")
	gitT(t, dir, "config", "user.name", "t")
	gitT(t, dir, "config", "commit.gpgsign", "false")
	writeFile(t, filepath.Join(dir, "deps.txt"), "a>=1.0.0\n")
	gitT(t, dir, "add", ".")
	gitT(t, dir, "commit", "-qm", "init")
	return dir
}

func writeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestRepos(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"acme-api", "acme-web", "other"} {
		newRepo(t, root, name)
	}
	if err := os.Mkdir(filepath.Join(root, "acme-notgit"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(root, "acme-file"), "")

	got, err := Repos(root, regexp.MustCompile(`^acme-`), regexp.MustCompile(`web`))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != filepath.Join(root, "acme-api") {
		t.Errorf("repos = %v", got)
	}
	if got, _ := Repos(root, regexp.MustCompile(`.*`), nil); len(got) != 3 {
		t.Errorf("all repos = %v", got)
	}
}

// bump stands in for doneram's update: it rewrites deps.txt when applying.
func bump(_ context.Context, dir string, apply bool) (*Result, error) {
	if apply {
		if err := os.WriteFile(filepath.Join(dir, "deps.txt"), []byte("a>=2.0.0\n"), 0o644); err != nil {
			return nil, err
		}
	}
	return &Result{Counts: Counts{Updates: 1}, Changes: []string{"a 1.0.0 -> 2.0.0"}}, nil
}

func TestRunReportsWithoutTouching(t *testing.T) {
	dir := newRepo(t, t.TempDir(), "app")
	report := Run(context.Background(), []string{dir}, Options{Workers: 2}, bump)
	rep := report.Repos[0]
	if rep.Status != StatusOK || rep.Counts.Updates != 1 || rep.Repo != "app" || rep.Commit != "" {
		t.Errorf("report = %+v", rep)
	}
	if out := gitT(t, dir, "branch", "--list", Branch); out != "" {
		t.Errorf("a report-only run made %s", Branch)
	}
	if md := report.Markdown(); !strings.Contains(md, "a 1.0.0 -> 2.0.0") || !strings.Contains(md, "| `app` | 1 |") {
		t.Errorf("markdown:\n%s", md)
	}
}

func TestRunApplyCommitsOnBranch(t *testing.T) {
	root := t.TempDir()
	clean := newRepo(t, root, "clean")
	gitT(t, clean, "switch", "-q", "-c", "feature")
	head := gitT(t, clean, "rev-parse", "HEAD")
	dirty := newRepo(t, root, "dirty")
	writeFile(t, filepath.Join(dirty, "deps.txt"), "local edit\n")
	failing := newRepo(t, root, "failing")

	check := func(ctx context.Context, dir string, apply bool) (*Result, error) {
		if filepath.Base(dir) == "failing" {
			// A half-applied tree: nothing of it may survive.
			writeFile(t, filepath.Join(dir, "deps.txt"), "half\n")
			writeFile(t, filepath.Join(dir, "new.txt"), "half\n")
			return nil, errors.New("patch failed")
		}
		return bump(ctx, dir, apply)
	}
	report := Run(context.Background(), []string{clean, dirty, failing}, Options{Apply: true, Workers: 3}, check)

	rep := report.Repos[0]
	if rep.Status != StatusOK || rep.Branch != Branch || rep.Commit == "" {
		t.Fatalf("clean report = %+v", rep)
	}
	if got := gitT(t, clean, "symbolic-ref", "--short", "HEAD"); got != "feature" {
		t.Errorf("checked out %q, want feature", got)
	}
	if got := gitT(t, clean, "rev-parse", "HEAD"); got != head {
		t.Error("the checked-out branch moved")
	}
	if got := gitT(t, clean, "status", "--porcelain"); got != "" {
		t.Errorf("working tree left dirty: %q", got)
	}
	if got := gitT(t, clean, "rev-parse", Branch); got != rep.Commit {
		t.Errorf("%s = %s, report says %s", Branch, got, rep.Commit)
	}
	if got := gitT(t, clean, "rev-parse", Branch+"^"); got != head {
		t.Errorf("%s is not one commit on the original HEAD", Branch)
	}
	if got := gitT(t, clean, "show", Branch+":deps.txt"); got != "a>=2.0.0" {
		t.Errorf("%s deps.txt = %q", Branch, got)
	}
	if got := gitT(t, clean, "log", "-1", "--format=%B", Branch); got != "chore: doneram updates\n\n- a 1.0.0 -> 2.0.0" {
		t.Errorf("commit message = %q", got)
	}

	if d := report.Repos[1]; d.Status != StatusSkipped || d.Skipped != "uncommitted changes" {
		t.Errorf("dirty report = %+v", d)
	}
	if data, _ := os.ReadFile(filepath.Join(dirty, "deps.txt")); string(data) != "local edit\n" {
		t.Error("the dirty repo's edit was touched")
	}

	f := report.Repos[2]
	if f.Status != StatusFailed || len(f.Errors) != 1 {
		t.Errorf("failing report = %+v", f)
	}
	if got := gitT(t, failing, "symbolic-ref", "--short", "HEAD"); got != "main" {
		t.Errorf("failing repo left on %q", got)
	}
	if got := gitT(t, failing, "status", "--porcelain"); got != "" {
		t.Errorf("failing repo left dirty: %q", got)
	}
	if got := gitT(t, failing, "branch", "--list", Branch); got != "" {
		t.Errorf("failing repo got %s", Branch)
	}
	if report.Failed() != 1 {
		t.Errorf("failed = %d", report.Failed())
	}
}

func TestRunApplyWithNothingToCommit(t *testing.T) {
	dir := newRepo(t, t.TempDir(), "app")
	none := func(context.Context, string, bool) (*Result, error) { return &Result{}, nil }
	rep := Run(context.Background(), []string{dir}, Options{Apply: true}, none).Repos[0]
	if rep.Status != StatusOK || rep.Commit != "" {
		t.Errorf("report = %+v", rep)
	}
	if out := gitT(t, dir, "branch", "--list", Branch); out != "" {
		t.Errorf("made %s with nothing to commit", Branch)
	}
	if got := gitT(t, dir, "symbolic-ref", "--short", "HEAD"); got != "main" {
		t.Errorf("left on %q", got)
	}
}

func TestRunApplyHonorsCommitHooks(t *testing.T) {
	dir := newRepo(t, t.TempDir(), "hooked")
	hook := filepath.Join(dir, ".git", "hooks", "pre-commit")
	if err := os.MkdirAll(filepath.Dir(hook), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(hook, []byte("#!/bin/sh\necho rejected >&2\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	head := gitT(t, dir, "rev-parse", "HEAD")
	rep := Run(context.Background(), []string{dir}, Options{Apply: true, Workers: 1}, bump).Repos[0]
	if rep.Status != StatusFailed || rep.Commit != "" {
		t.Errorf("report = %+v, want a failure from the hook", rep)
	}
	if out := gitT(t, dir, "branch", "--list", Branch); out != "" {
		t.Errorf("a rejected commit still made %s", Branch)
	}
	if got := gitT(t, dir, "rev-parse", "HEAD"); got != head {
		t.Errorf("HEAD moved to %s", got)
	}
	if status := gitT(t, dir, "status", "--porcelain"); status != "" {
		t.Errorf("tree left dirty: %s", status)
	}
}
