package fleet

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// Repo statuses. Only StatusFailed fails the fleet run: a repo that could
// not be cloned or planned at all. A group that failed is in the report
// and leaves the rest of its repo standing.
const (
	StatusOK      = "ok"
	StatusSkipped = "skipped"
	StatusFailed  = "failed"
)

// What happened to one group.
const (
	ActionPlanned   = "planned"
	ActionOpened    = "opened"
	ActionUpdated   = "updated"
	ActionUnchanged = "unchanged"
	ActionNoChanges = "no changes"
	ActionFailed    = "failed"
)

// Report is one fleet run across every repo.
type Report struct {
	DryRun   bool         `json:"dryRun"`
	Started  time.Time    `json:"started"`
	Finished time.Time    `json:"finished"`
	Repos    []RepoReport `json:"repos"`
}

// RepoReport is one repo's part of the run.
type RepoReport struct {
	Repo     string        `json:"repo"`
	Status   string        `json:"status"`
	Skipped  string        `json:"skipped,omitempty"`
	Counts   Counts        `json:"counts"`
	Groups   []GroupReport `json:"groups,omitempty"`
	Closed   []ClosedPR    `json:"closed,omitempty"`
	Errors   []string      `json:"errors,omitempty"`
	Warnings []string      `json:"warnings,omitempty"`
}

// GroupReport is one group's pull request, or what stopped it.
type GroupReport struct {
	ID      string `json:"id"`
	Branch  string `json:"branch"`
	Title   string `json:"title"`
	Action  string `json:"action"`
	Number  int    `json:"number,omitempty"`
	URL     string `json:"url,omitempty"`
	Error   string `json:"error,omitempty"`
	Warning string `json:"warning,omitempty"`
}

// ClosedPR is a stale doneram pull request this run closed.
type ClosedPR struct {
	Number int    `json:"number"`
	Branch string `json:"branch"`
	URL    string `json:"url,omitempty"`
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
// then each repo's pull requests and problems.
func (r *Report) Markdown() string {
	var b strings.Builder
	title := "doneram fleet"
	if r.DryRun {
		title += " (dry run)"
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
	fmt.Fprintf(&b, "%d repo(s): %d update(s), %d security fix(es), %d lockfile advisory(ies), %d floating reference(s), %d end-of-life item(s) (%d past).",
		len(r.Repos), total.Updates, total.Security, total.LockAdvisories, total.Floating, total.EOL, total.EOLPast)
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

	prs := "PRs"
	if r.DryRun {
		prs = "Groups"
	}
	fmt.Fprintf(&b, "| Repo | Updates | Security | Lockfile advisories | Floating | EOL (past) | %s | Status |\n", prs)
	b.WriteString("| --- | --: | --: | --: | --: | --: | --- | --- |\n")
	for _, repo := range r.Repos {
		c := repo.Counts
		fmt.Fprintf(&b, "| `%s` | %d | %d | %d | %d | %d (%d) | %s | %s |\n",
			repo.Repo, c.Updates, c.Security, c.LockAdvisories, c.Floating, c.EOL, c.EOLPast, groupCell(repo, r.DryRun), statusCell(repo))
	}

	for _, repo := range r.Repos {
		if len(repo.Groups) == 0 && len(repo.Closed) == 0 && len(repo.Errors) == 0 && len(repo.Warnings) == 0 {
			continue
		}
		fmt.Fprintf(&b, "\n### `%s`\n\n", repo.Repo)
		for _, g := range repo.Groups {
			fmt.Fprintf(&b, "- `%s`: %s", g.ID, g.Action)
			if g.URL != "" {
				fmt.Fprintf(&b, " [#%d](%s)", g.Number, g.URL)
			}
			if g.Title != "" {
				fmt.Fprintf(&b, ", %s", oneLine(g.Title))
			}
			if g.Error != "" {
				fmt.Fprintf(&b, "\n  - error: %s", oneLine(g.Error))
			}
			if g.Warning != "" {
				fmt.Fprintf(&b, "\n  - warning: %s", oneLine(g.Warning))
			}
			b.WriteString("\n")
		}
		for _, c := range repo.Closed {
			fmt.Fprintf(&b, "- closed stale #%d (`%s`)\n", c.Number, c.Branch)
		}
		for _, e := range repo.Errors {
			fmt.Fprintf(&b, "- error: %s\n", oneLine(e))
		}
		for _, w := range repo.Warnings {
			fmt.Fprintf(&b, "- warning: %s\n", oneLine(w))
		}
	}
	return b.String()
}

func groupCell(repo RepoReport, dryRun bool) string {
	var parts []string
	for _, g := range repo.Groups {
		switch {
		case g.URL != "":
			parts = append(parts, fmt.Sprintf("[#%d](%s) %s", g.Number, g.URL, g.Action))
		case dryRun:
			parts = append(parts, "`"+g.ID+"`")
		default:
			parts = append(parts, fmt.Sprintf("`%s` %s", g.ID, g.Action))
		}
	}
	return strings.Join(parts, "<br>")
}

func statusCell(repo RepoReport) string {
	switch repo.Status {
	case StatusSkipped:
		return "skipped: " + repo.Skipped
	case StatusFailed:
		return "**failed**"
	}
	failed := 0
	for _, g := range repo.Groups {
		if g.Action == ActionFailed {
			failed++
		}
	}
	if failed > 0 || len(repo.Errors) > 0 {
		return fmt.Sprintf("ok, %d error(s)", failed+len(repo.Errors))
	}
	return "ok"
}

// oneLine keeps a multi-line error inside its list item and table cell.
func oneLine(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	return strings.ReplaceAll(s, "|", `\|`)
}

func add(a, b Counts) Counts {
	return Counts{
		Updates:        a.Updates + b.Updates,
		Security:       a.Security + b.Security,
		LockAdvisories: a.LockAdvisories + b.LockAdvisories,
		Floating:       a.Floating + b.Floating,
		EOL:            a.EOL + b.EOL,
		EOLPast:        a.EOLPast + b.EOLPast,
	}
}
