package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/kyleking/doneram/internal/eol"
	"github.com/kyleking/doneram/internal/lifecycle"
)

// eolWarnDays is how close to its end of life a reference is reported.
// Past it, a reference is quiet: a date years out needs no attention.
const eolWarnDays = 90

// pklLifecycleSummary is one reference past or nearing its end of life.
type pklLifecycleSummary struct {
	Source   string `json:"source"`
	File     string `json:"file"`
	Line     int    `json:"line"`
	Name     string `json:"name"`
	Product  string `json:"product"`
	Cycle    string `json:"cycle"`
	Date     string `json:"date,omitempty"`
	DaysLeft *int   `json:"daysLeft,omitempty"`
	Past     bool   `json:"past"`
	// Inferred marks a verdict drawn from the project's release history
	// rather than a published end-of-life date; Reason says why. An
	// inferred entry is Past only when the history is confident.
	Inferred bool   `json:"inferred,omitempty"`
	Reason   string `json:"reason,omitempty"`
}

// checkLifecycle looks up every lifecycle reference under baseDir and
// returns the ones past or within eolWarnDays of end of life. A reference
// no cycle covers (an unreleased runtime, a string that only looked like a
// model ID) is skipped rather than reported, and so is a line a config
// site already checks with its own `eol`.
func checkLifecycle(ctx context.Context, out report, baseDir string, client *eol.Client, now time.Time, covered map[string]bool) ([]pklLifecycleSummary, error) {
	refs, err := lifecycle.Scan(baseDir)
	if err != nil {
		return nil, fmt.Errorf("scanning for runtimes: %w", err)
	}
	var flagged []pklLifecycleSummary
	for _, r := range refs {
		if covered[fmt.Sprintf("%s:%d", r.File, r.Line)] {
			continue
		}
		var status eol.Status
		if r.Product == lifecycle.GitHubAPI {
			supported, err := githubAPIVersions(ctx)
			if err != nil || supported[r.Version] {
				continue
			}
			flagged = append(flagged, pklLifecycleSummary{Source: string(r.Source), File: relTo(baseDir, r.File), Line: r.Line, Name: r.Name, Product: "GitHub REST API", Cycle: r.Version, Past: true})
			continue
		}
		if r.ByID {
			status, err = client.LookupID(ctx, r.Product, r.Version)
		} else {
			status, err = client.Lookup(ctx, r.Product, r.Version)
		}
		if err != nil {
			continue
		}
		s := pklLifecycleSummary{Source: string(r.Source), Line: r.Line, Name: r.Name, Product: status.Product, Cycle: status.Cycle, Past: status.Past(now)}
		if rel, err := filepath.Rel(baseDir, r.File); err == nil {
			s.File = filepath.ToSlash(rel)
		}
		if !status.Date.IsZero() {
			days := status.DaysLeft(now)
			s.Date, s.DaysLeft = status.Date.Format(time.DateOnly), &days
		}
		if !s.Past && (s.DaysLeft == nil || *s.DaysLeft > eolWarnDays) {
			continue
		}
		flagged = append(flagged, s)
	}
	if len(flagged) > 0 {
		out.printf("\nEnd of life:\n")
		for _, s := range flagged {
			printLifecycle(out, s)
		}
	}
	return flagged, nil
}

func lifecycleLine(s pklLifecycleSummary) string {
	switch {
	case s.Inferred && s.Past:
		return fmt.Sprintf("%s %s looks unmaintained: %s", s.Product, s.Cycle, s.Reason)
	case s.Inferred:
		return fmt.Sprintf("%s %s may be unmaintained: %s", s.Product, s.Cycle, s.Reason)
	case s.Past && s.Date != "":
		return fmt.Sprintf("%s %s reached end of life on %s", s.Product, s.Cycle, s.Date)
	case s.Past:
		return fmt.Sprintf("%s %s has reached end of life", s.Product, s.Cycle)
	default:
		return fmt.Sprintf("%s %s ends %s (%d days)", s.Product, s.Cycle, s.Date, *s.DaysLeft)
	}
}

func writeLifecycleSection(body *strings.Builder, items []pklLifecycleSummary) {
	if len(items) == 0 {
		return
	}
	body.WriteString("\n### End of life\n\n")
	fmt.Fprintf(body, "Not changed by this PR: moving a runtime, base image, runner, or model to a supported release is a migration to plan. Listed when past or within %d days of end of life.\n\n", eolWarnDays)
	body.WriteString("| Reference | Product | Status | Where |\n")
	body.WriteString("| --- | --- | --- | --- |\n")
	for _, s := range items {
		fmt.Fprintf(body, "| `%s` | %s %s | %s | `%s:%d` |\n", s.Name, s.Product, s.Cycle, lifecycleStatus(s), s.File, s.Line)
	}
}

func lifecycleStatus(s pklLifecycleSummary) string {
	switch {
	case s.Inferred && s.Past:
		return "**unmaintained** (inferred: " + s.Reason + ")"
	case s.Inferred:
		return "likely unmaintained (inferred: " + s.Reason + ")"
	case s.Past && s.Date != "":
		return "**ended " + s.Date + "**"
	case s.Past:
		return "**ended**"
	default:
		return fmt.Sprintf("ends %s (%d days)", s.Date, *s.DaysLeft)
	}
}

// githubAPIBase is GitHub's REST API, overridden in tests.
var githubAPIBase = "https://api.github.com"

var (
	githubVersionsOnce sync.Once
	githubVersions     map[string]bool
	githubVersionsErr  error
)

// githubAPIVersions returns the REST API versions GitHub still accepts, read
// once per process from its /versions endpoint.
func githubAPIVersions(ctx context.Context) (map[string]bool, error) {
	githubVersionsOnce.Do(func() {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, githubAPIBase+"/versions", nil)
		if err != nil {
			githubVersionsErr = err
			return
		}
		if token := os.Getenv("GITHUB_TOKEN"); token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			githubVersionsErr = err
			return
		}
		defer func() { _ = resp.Body.Close() }()
		var list []string
		if resp.StatusCode != http.StatusOK {
			githubVersionsErr = fmt.Errorf("GitHub /versions: status %d", resp.StatusCode)
			return
		}
		if err := json.NewDecoder(resp.Body).Decode(&list); err != nil {
			githubVersionsErr = err
			return
		}
		githubVersions = map[string]bool{}
		for _, v := range list {
			githubVersions[v] = true
		}
	})
	return githubVersions, githubVersionsErr
}

func relTo(baseDir, path string) string {
	if rel, err := filepath.Rel(baseDir, path); err == nil {
		return filepath.ToSlash(rel)
	}
	return path
}

func printLifecycle(out report, s pklLifecycleSummary) {
	out.printf("%s %s:%d %s: %s\n", map[bool]string{true: "✗", false: "⚠"}[s.Past], s.File, s.Line, s.Name, lifecycleLine(s))
}
