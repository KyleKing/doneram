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
// defaults, so it runs unattended. It does not pass --trust: a template
// whose update runs tasks or migrations fails here, and the report says
// to run it by hand, since running a template's code is a decision for a
// person, not a scheduled job.
func Update(ctx context.Context, t Template, tag string) (string, error) {
	cmd := exec.CommandContext(ctx, "copier", "update", "--defaults", "--skip-answered",
		"--vcs-ref", tag, "--answers-file", filepath.Base(t.AnswersFile))
	cmd.Dir = filepath.Dir(t.AnswersFile)
	out, err := cmd.CombinedOutput()
	return strings.TrimSpace(string(out)), err
}
