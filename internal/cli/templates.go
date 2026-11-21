package cli

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
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
//
// A template owned by the project's own GitHub account, or listed in
// trustTemplates, updates with --trust so its tasks and migrations run.
func checkTemplates(ctx context.Context, out report, baseDir string, apply bool, releases resolver.Resolver, trusted []string) ([]pklTemplateSummary, int) {
	templates, err := copier.Scan(baseDir)
	if err != nil {
		out.printf("? copier templates: %v\n", err)
		return nil, 1
	}
	var found []pklTemplateSummary
	failures := 0
	owner := ""
	if len(templates) > 0 {
		owner = copier.RepoOwner(ctx, baseDir)
	}
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
		trust := copier.Trusted(t, owner, trusted)
		s.Command = fmt.Sprintf("copier update --defaults --skip-answered --vcs-ref %s --answers-file %s", latest, relTo(baseDir, t.AnswersFile))
		if trust {
			s.Command += " --trust"
		}
		switch {
		case !apply:
			out.printf("→ template %s: %s -> %s\n", t.Source, t.Commit, latest)
		case !copierInstalled():
			out.printf("  copier not installed; run `%s`\n", s.Command)
		default:
			if res, err := copier.Update(ctx, t, latest, trust); err != nil {
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

// templateOwnership maps each file a copier template keeps maintaining in
// this project, by absolute path, to the template's source. Pins in those
// files are the template's to bump: a release of the template carries
// them to every project at once, and `copier update` brings them here.
// A template that cannot be listed owns nothing, so its files are checked
// like the project's own.
func templateOwnership(ctx context.Context, out report, baseDir string, src copier.Source) map[string]string {
	templates, err := copier.Scan(baseDir)
	if err != nil || len(templates) == 0 {
		return nil
	}
	owned := map[string]string{}
	for _, t := range templates {
		files, settings, err := src.Files(ctx, t)
		if err != nil {
			out.printf("? copier template %s: %v; its files are checked here\n", t.Source, err)
			continue
		}
		s, err := copier.ParseSettings(settings)
		if err != nil {
			out.printf("? copier template %s: parsing copier.yml: %v\n", t.Source, err)
			continue
		}
		root := filepath.Dir(t.AnswersFile)
		for _, rel := range copier.Owned(files, s, filepath.Base(t.AnswersFile)) {
			abs, err := filepath.Abs(filepath.Join(root, filepath.FromSlash(rel)))
			if err != nil {
				continue
			}
			if _, err := os.Stat(abs); err == nil {
				owned[abs] = t.Source
			}
		}
	}
	return owned
}

// ownedBy returns the template that maintains file, if any.
func ownedBy(owned map[string]string, file string) (string, bool) {
	if len(owned) == 0 {
		return "", false
	}
	abs, err := filepath.Abs(file)
	if err != nil {
		return "", false
	}
	src, ok := owned[abs]
	return src, ok
}

// leaveToTemplates drops the discovered sites in template-maintained files
// and reports how many each template keeps.
func leaveToTemplates(out report, owned map[string]string, sites []engine.Site) []engine.Site {
	if len(owned) == 0 {
		return sites
	}
	kept := sites[:0:0]
	left := map[string]int{}
	for _, s := range sites {
		if src, ok := ownedBy(owned, s.Locator.Glob); ok {
			left[src]++
			continue
		}
		kept = append(kept, s)
	}
	srcs := make([]string, 0, len(left))
	for src := range left {
		srcs = append(srcs, src)
	}
	sort.Strings(srcs)
	for _, src := range srcs {
		out.printf("  %d pin(s) left to template %s, which releases them to this project\n", left[src], src)
	}
	return kept
}
