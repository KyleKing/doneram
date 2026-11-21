package fleet

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// Repo statuses. Only StatusFailed fails the fleet run: a repo whose run
// did not finish, or whose git steps failed.
const (
	StatusOK      = "ok"
	StatusSkipped = "skipped"
	StatusFailed  = "failed"
)

// Report is one fleet run across every repo.
type Report struct {
	Apply    bool         `json:"apply"`
	Started  time.Time    `json:"started"`
	Finished time.Time    `json:"finished"`
	Repos    []RepoReport `json:"repos"`
}

// RepoReport is one repo's part of the run.
type RepoReport struct {
	Repo     string   `json:"repo"`
	Path     string   `json:"path"`
	Status   string   `json:"status"`
	Skipped  string   `json:"skipped,omitempty"`
	Counts   Counts   `json:"counts"`
	Changes  []string `json:"changes,omitempty"`
	Findings []string `json:"findings,omitempty"`
	// Branch and Commit are set when an apply run committed changes.
	Branch string   `json:"branch,omitempty"`
	Commit string   `json:"commit,omitempty"`
	Errors []string `json:"errors,omitempty"`
}

// Failed counts the repos that failed outright.
func (r *Report) Failed() int {
	n := 0
	for _, repo := range r.Repos {
		if repo.Status == StatusFailed {
			n++
		}
	}
	return n
}

// JSON renders the report for a script to read.
func (r *Report) JSON() ([]byte, error) {
	data, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}

// Markdown renders the report for a person: one table across the fleet,
// then each repo's changes, findings, and errors.
func (r *Report) Markdown() string {
	var b strings.Builder
	title := "doneram fleet"
	if r.Apply {
		title += " --apply"
	}
	fmt.Fprintf(&b, "## %s\n\n", title)

	var total Counts
	failed, skipped := 0, 0
	for _, repo := range r.Repos {
		total = add(total, repo.Counts)
		switch repo.Status {
		case StatusFailed:
			failed++
		case StatusSkipped:
			skipped++
		}
	}
	fmt.Fprintf(&b, "%d repo(s): %d update(s), %d vulnerable, %d floating reference(s), %d end-of-life item(s) (%d past).",
		len(r.Repos), total.Updates, total.Vulnerable, total.Floating, total.EOL, total.EOLPast)
	if failed > 0 {
		fmt.Fprintf(&b, " **%d failed.**", failed)
	}
	if skipped > 0 {
		fmt.Fprintf(&b, " %d skipped.", skipped)
	}
	b.WriteString("\n\n")
	if len(r.Repos) == 0 {
		return b.String()
	}

	b.WriteString("| Repo | Updates | Vulnerable | Floating | EOL (past) | Status |\n")
	b.WriteString("| --- | --: | --: | --: | --: | --- |\n")
	for _, repo := range r.Repos {
		c := repo.Counts
		fmt.Fprintf(&b, "| `%s` | %d | %d | %d | %d (%d) | %s |\n",
			repo.Repo, c.Updates, c.Vulnerable, c.Floating, c.EOL, c.EOLPast, oneLine(statusText(repo)))
	}

	for _, repo := range r.Repos {
		if len(repo.Changes) == 0 && len(repo.Findings) == 0 && len(repo.Errors) == 0 {
			continue
		}
		fmt.Fprintf(&b, "\n### `%s`\n\n", repo.Repo)
		for _, c := range repo.Changes {
			fmt.Fprintf(&b, "- %s\n", oneLine(c))
		}
		for _, f := range repo.Findings {
			fmt.Fprintf(&b, "- %s\n", oneLine(f))
		}
		for _, e := range repo.Errors {
			fmt.Fprintf(&b, "- error: %s\n", oneLine(e))
		}
	}
	return b.String()
}

func statusText(repo RepoReport) string {
	var s string
	switch {
	case repo.Status == StatusSkipped:
		return "skipped: " + repo.Skipped
	case repo.Status == StatusFailed:
		return "failed"
	case repo.Commit != "":
		s = fmt.Sprintf("committed %.12s on %s", repo.Commit, repo.Branch)
	default:
		s = "ok"
	}
	if n := len(repo.Errors); n > 0 {
		s += fmt.Sprintf(", %d error(s)", n)
	}
	return s
}

// oneLine keeps a multi-line error inside its list item and table cell.
func oneLine(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	return strings.ReplaceAll(s, "|", `\|`)
}

func add(a, b Counts) Counts {
	return Counts{
		Updates:    a.Updates + b.Updates,
		Vulnerable: a.Vulnerable + b.Vulnerable,
		Floating:   a.Floating + b.Floating,
		EOL:        a.EOL + b.EOL,
		EOLPast:    a.EOLPast + b.EOLPast,
	}
}
