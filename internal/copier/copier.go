// Package copier treats a project generated from a copier template as
// pinned to a template release: `_commit` in .copier-answers.yml is the
// pin, and the template repository's releases are what it can move to.
// Applying the move is copier's job (`copier update`), which re-renders the
// template and merges it with local edits; doneram only notices the drift
// and runs it.
package copier

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

// Template is one project's link to the template it was generated from.
type Template struct {
	// AnswersFile is the .copier-answers*.yml path.
	AnswersFile string
	// Source is `_src_path` as written ("gh:owner/repo",
	// "https://github.com/owner/repo.git").
	Source string
	// Repo is the GitHub "owner/repo" Source names, empty for a template
	// hosted anywhere else.
	Repo string
	// Commit is `_commit`: the template release the project was last
	// rendered from.
	Commit string
}

var answersName = regexp.MustCompile(`^\.copier-answers.*\.ya?ml$`)

// githubSource reads "gh:owner/repo", "https://github.com/owner/repo(.git)",
// or "git@github.com:owner/repo(.git)".
var githubSource = regexp.MustCompile(`^(?:gh:|https://github\.com/|git@github\.com:|git\+https://github\.com/)([A-Za-z0-9_.-]+)/([A-Za-z0-9_.-]+?)(?:\.git)?/?$`)

// Scan returns every template link under baseDir. Answers files live at a
// project's root, so only baseDir and its direct subdirectories are read.
func Scan(baseDir string) ([]Template, error) {
	var dirs = []string{baseDir}
	entries, err := os.ReadDir(baseDir)
	if err != nil {
		return nil, err
	}
	for _, e := range entries {
		if e.IsDir() && !strings.HasPrefix(e.Name(), ".") && e.Name() != "node_modules" {
			dirs = append(dirs, filepath.Join(baseDir, e.Name()))
		}
	}

	var out []Template
	for _, dir := range dirs {
		files, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, f := range files {
			if f.IsDir() || !answersName.MatchString(f.Name()) {
				continue
			}
			path := filepath.Join(dir, f.Name())
			data, err := os.ReadFile(path)
			if err != nil {
				return nil, err
			}
			var answers struct {
				Commit  string `yaml:"_commit"`
				SrcPath string `yaml:"_src_path"`
			}
			if err := yaml.Unmarshal(data, &answers); err != nil {
				return nil, fmt.Errorf("parsing %s: %w", path, err)
			}
			if answers.Commit == "" || answers.SrcPath == "" {
				continue
			}
			t := Template{AnswersFile: path, Source: answers.SrcPath, Commit: answers.Commit}
			if m := githubSource.FindStringSubmatch(answers.SrcPath); m != nil {
				t.Repo = m[1] + "/" + m[2]
			}
			out = append(out, t)
		}
	}
	return out, nil
}

// Released reports whether a `_commit` names a release tag rather than a
// commit between releases ("v1.2.3-4-gabcdef"), which has no version to
// compare against.
func Released(commit string) bool {
	return regexp.MustCompile(`^v?\d+(\.\d+)*$`).MatchString(commit)
}

// Update runs `copier update` to move a project to the template release at
// tag. Answers already given are reused and new questions take their
// defaults, so it runs unattended. trust passes --trust, which lets the
// template's tasks and migrations run; without it, a template that has
// any fails here, and the report says to run it by hand, since running
// someone else's template code is a decision for a person, not a
// scheduled job.
func Update(ctx context.Context, t Template, tag string, trust bool) (string, error) {
	args := []string{"update", "--defaults", "--skip-answered", "--vcs-ref", tag, "--answers-file", filepath.Base(t.AnswersFile)}
	if trust {
		args = append(args, "--trust")
	}
	cmd := exec.CommandContext(ctx, "copier", args...)
	cmd.Dir = filepath.Dir(t.AnswersFile)
	out, err := cmd.CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

// ownerOf matches the account in a GitHub remote URL, with or without
// credentials ("https://x-access-token:...@github.com/owner/repo").
var ownerOf = regexp.MustCompile(`^(?:https://(?:[^@/]+@)?github\.com/|git@github\.com:|ssh://git@github\.com/)([A-Za-z0-9_.-]+)/`)

// RepoOwner returns the GitHub account that owns the repository checked out
// at dir: its origin remote, or GITHUB_REPOSITORY in Actions when the
// remote is not a GitHub URL. Empty when neither says.
func RepoOwner(ctx context.Context, dir string) string {
	out, err := exec.CommandContext(ctx, "git", "-C", dir, "remote", "get-url", "origin").Output()
	if err == nil {
		if m := ownerOf.FindStringSubmatch(strings.TrimSpace(string(out))); m != nil {
			return m[1]
		}
	}
	if repo := os.Getenv("GITHUB_REPOSITORY"); strings.Contains(repo, "/") {
		return strings.SplitN(repo, "/", 2)[0]
	}
	return ""
}

// Trusted reports whether a template's tasks may run unattended: it is
// listed, or it lives under the same GitHub account as the project, whose
// owner wrote it.
func Trusted(t Template, owner string, listed []string) bool {
	for _, src := range listed {
		if src == t.Source || (t.Repo != "" && strings.EqualFold(strings.TrimPrefix(src, "gh:"), t.Repo)) {
			return true
		}
	}
	return owner != "" && t.Repo != "" && strings.EqualFold(strings.SplitN(t.Repo, "/", 2)[0], owner)
}
