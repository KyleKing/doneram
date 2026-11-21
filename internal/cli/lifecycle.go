package cli

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
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
			out.printf("%s %s:%d %s: %s\n", map[bool]string{true: "✗", false: "⚠"}[s.Past], s.File, s.Line, s.Name, lifecycleLine(s))
		}
	}
	return flagged, nil
}

func lifecycleLine(s pklLifecycleSummary) string {
	switch {
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
	case s.Past && s.Date != "":
		return "**ended " + s.Date + "**"
	case s.Past:
		return "**ended**"
	default:
		return fmt.Sprintf("ends %s (%d days)", s.Date, *s.DaysLeft)
	}
}
