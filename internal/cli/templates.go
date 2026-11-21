package cli

import (
	"context"
	"fmt"
	"os/exec"
	"strings"

	"github.com/kyleking/doneram/internal/copier"
	"github.com/kyleking/doneram/internal/engine"
	"github.com/kyleking/doneram/internal/parser"
	"github.com/kyleking/doneram/internal/resolver"
)

// pklTemplateSummary is one project generated from a copier template that
// is behind the template's newest release.
type pklTemplateSummary struct {
	File     string `json:"file"`
	Template string `json:"template"`
	Current  string `json:"current"`
	Latest   string `json:"latest"`
	Updated  bool   `json:"updated"`
	Command  string `json:"command,omitempty"`
	Error    string `json:"error,omitempty"`
}

// checkTemplates finds projects behind their copier template's newest
// release and, when applying, runs `copier update` for each. It runs before
// any pin is patched, so the pins the template renders are then checked
// like any other.
func checkTemplates(ctx context.Context, out report, baseDir string, apply bool, releases resolver.Resolver) ([]pklTemplateSummary, int) {
	templates, err := copier.Scan(baseDir)
	if err != nil {
		out.printf("? copier templates: %v\n", err)
		return nil, 1
	}
	var found []pklTemplateSummary
	failures := 0
	for _, t := range templates {
		if t.Repo == "" || !copier.Released(t.Commit) {
			continue
		}
		latest, err := releases.Resolve(ctx, t.Repo, parser.ParsePattern("#.#.#"))
		if err != nil {
			out.printf("? copier template %s: %v\n", t.Repo, err)
			continue
		}
		if latest == "" {
			continue
		}
		if strings.HasPrefix(t.Commit, "v") && !strings.HasPrefix(latest, "v") {
			latest = "v" + latest
		}
		if !engine.IsOlder(t.Commit, latest) {
			continue
		}
		s := pklTemplateSummary{File: relTo(baseDir, t.AnswersFile), Template: t.Source, Current: t.Commit, Latest: latest}
		s.Command = fmt.Sprintf("copier update --defaults --skip-answered --vcs-ref %s --answers-file %s", latest, relTo(baseDir, t.AnswersFile))
		switch {
		case !apply:
			out.printf("→ template %s: %s -> %s\n", t.Source, t.Commit, latest)
		case !copierInstalled():
			out.printf("  copier not installed; run `%s`\n", s.Command)
		default:
			if res, err := copier.Update(ctx, t, latest); err != nil {
				s.Error = fmt.Sprintf("%v: %s", err, tail(res, 20))
				out.printf("✗ copier update %s: %s\n", t.Source, s.Error)
				failures++
			} else {
				s.Updated = true
				out.printf("  updated %s from template %s %s -> %s\n", s.File, t.Source, t.Commit, latest)
			}
		}
		found = append(found, s)
	}
	return found, failures
}

func copierInstalled() bool {
	_, err := exec.LookPath("copier")
	return err == nil
}

func writeTemplateSection(body *strings.Builder, items []pklTemplateSummary) {
	var rows []pklTemplateSummary
	for _, t := range items {
		if t.Updated {
			rows = append(rows, t)
		}
	}
	if len(rows) == 0 {
		return
	}
	body.WriteString("\n### Template updates\n\n")
	body.WriteString("Re-rendered by `copier update` and merged with local edits. Look for conflict markers or `.rej` files before merging.\n\n")
	body.WriteString("| Template | Current | Latest | Answers |\n")
	body.WriteString("| --- | --- | --- | --- |\n")
	for _, t := range rows {
		fmt.Fprintf(body, "| `%s` | `%s` | `%s` | `%s` |\n", t.Template, t.Current, t.Latest, t.File)
	}
}
