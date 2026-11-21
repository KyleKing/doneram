package cli

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/kyleking/doneram/internal/actionrisk"
	"github.com/kyleking/doneram/internal/manifest"
	"github.com/kyleking/doneram/internal/parser"
	"github.com/kyleking/doneram/internal/resolver"
)

// pklManifestSummary is one project whose lock was upgraded within its
// declared ranges and whose declared floors were raised to match.
type pklManifestSummary struct {
	Dir     string `json:"dir"`
	Tool    string `json:"tool"`
	Changed bool   `json:"changed"`
	// Floors are the declared constraints raised, Go requires included.
	Floors []pklFloorSummary `json:"floors,omitempty"`
	// Command is what to run by hand when the package manager is not
	// installed here.
	Command string `json:"command,omitempty"`
	Error   string `json:"error,omitempty"`
}

type pklFloorSummary struct {
	File string `json:"file"`
	Name string `json:"name"`
	Old  string `json:"old"`
	New  string `json:"new"`
}

// manifestMajor is a release outside a declared range, proposed as a
// review bump that widens the range itself.
type manifestMajor struct {
	project manifest.Project
	file    string
	tool    string
	name    string
	old     string
	spec    string
	latest  string
}

// manifestWorkers bounds concurrent registry lookups for declared ranges;
// a package.json can declare a hundred of them.
const manifestWorkers = 8

func relPath(baseDir, path string) string {
	rel, err := filepath.Rel(baseDir, path)
	if err != nil {
		return filepath.ToSlash(path)
	}
	return filepath.ToSlash(rel)
}

// findManifestMajors checks the newest release of every capped declared
// range and returns the ones the range excludes. A package the registry
// does not know (a private one, usually) is reported and skipped, not
// counted as a failure, since nothing doneram could do would resolve it.
func findManifestMajors(ctx context.Context, out report, baseDir string, projects []manifest.Project, lookup func(string) (resolver.Resolver, bool)) []manifestMajor {
	type job struct {
		project manifest.Project
		c       manifest.Constraint
	}
	var jobs []job
	for _, p := range projects {
		cs, err := p.Constraints()
		if err != nil {
			out.printf("? %s: %v\n", relPath(baseDir, filepath.Join(p.Dir, p.Manifest)), err)
			continue
		}
		for _, c := range cs {
			jobs = append(jobs, job{p, c})
		}
	}

	pattern := parser.ParsePattern("#.#.#")
	found := make([]*manifestMajor, len(jobs))
	errs := make([]error, len(jobs))
	sem := make(chan struct{}, manifestWorkers)
	var wg sync.WaitGroup
	for i, j := range jobs {
		r, ok := lookup(j.c.Ecosystem)
		if !ok {
			continue
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			latest, err := r.Resolve(ctx, j.c.Name, pattern)
			if err != nil {
				errs[i] = err
				return
			}
			spec, ok := manifest.Widen(j.c, latest)
			if !ok {
				return
			}
			found[i] = &manifestMajor{
				project: j.project,
				file:    relPath(baseDir, filepath.Join(j.project.Dir, j.project.Manifest)),
				tool:    j.c.Ecosystem + ":" + j.c.Name,
				name:    j.c.Name,
				old:     j.c.Spec,
				spec:    spec,
				latest:  latest,
			}
		}()
	}
	wg.Wait()

	var majors []manifestMajor
	for i, m := range found {
		if errs[i] != nil {
			out.printf("? %s:%s in %s: %v\n", jobs[i].c.Ecosystem, jobs[i].c.Name, relPath(baseDir, filepath.Join(jobs[i].project.Dir, jobs[i].project.Manifest)), errs[i])
			continue
		}
		if m != nil {
			majors = append(majors, *m)
			out.printf("→ %s: %s -> %s (%s is outside the range %s declares)\n", m.tool, m.old, m.spec, m.latest, m.file)
		}
	}
	return majors
}

// majorRows are the summary rows for majors: each a review bump, so a plan
// run gives it a review-<tool> group of its own.
func majorRows(majors []manifestMajor) []pklSiteSummary {
	rows := make([]pklSiteSummary, len(majors))
	for i, m := range majors {
		rows[i] = pklSiteSummary{
			Tool:        m.tool,
			Current:     m.old,
			Latest:      m.spec,
			Detail:      m.file,
			Risk:        string(actionrisk.Review),
			RiskNotes:   []string{fmt.Sprintf("`%s` %s is outside the range `%s` declares in `%s`; the range is widened and the lock re-resolved", m.name, m.latest, m.old, m.file)},
			needsUpdate: true,
		}
	}
	return rows
}

// applyManifestMajors widens the ranges of the majors in g and relocks each
// project it touched. rows parallels majors and is marked as applied.
func applyManifestMajors(ctx context.Context, out report, majors []manifestMajor, rows []pklSiteSummary, g *pklGroup) (failures int) {
	var touched []manifest.Project
	seen := map[string]bool{}
	byDir := map[string][]int{}
	for i, m := range majors {
		if !g.has(m.tool) {
			continue
		}
		if !m.project.Installed() {
			out.printf("  %s not installed; set %s to %q in %s, then run `%s`\n", m.project.Tool, m.name, m.spec, m.file, strings.Join(m.project.RelockCommand(), " "))
			continue
		}
		changed, err := m.project.SetSpec(m.name, m.old, m.spec)
		if err != nil {
			rows[i].Error = err.Error()
			out.printf("✗ %s: %v\n", m.tool, err)
			failures++
			continue
		}
		if !changed {
			continue
		}
		out.printf("  widened %s to %s in %s\n", m.name, m.spec, m.file)
		rows[i].Updated = true
		byDir[m.project.Dir] = append(byDir[m.project.Dir], i)
		if !seen[m.project.Dir] {
			seen[m.project.Dir] = true
			touched = append(touched, m.project)
		}
	}
	for _, p := range touched {
		if err := manifest.Relock(ctx, manifest.Exec, p); err != nil {
			out.printf("✗ relocking %s: %v\n", p.Dir, err)
			for _, i := range byDir[p.Dir] {
				rows[i].Error = err.Error()
			}
			failures++
		}
	}
	return failures
}

// syncManifests upgrades each project's lock within its declared ranges,
// raises its declared floors to what is now locked, and relocks when a
// floor moved. A project whose package manager is not installed is
// reported with the command to run instead.
func syncManifests(ctx context.Context, out report, baseDir string, projects []manifest.Project) ([]pklManifestSummary, int) {
	var summaries []pklManifestSummary
	failures := 0
	for _, p := range projects {
		s := pklManifestSummary{Dir: relPath(baseDir, p.Dir), Tool: p.Tool}
		if !p.Installed() {
			s.Command = strings.Join(p.UpgradeCommand(), " ")
			out.printf("  %s not installed; run `%s` in %s, then raise its declared floors\n", p.Tool, s.Command, s.Dir)
			summaries = append(summaries, s)
			continue
		}
		before := p.Fingerprint()
		raises, err := syncProject(ctx, p)
		file := relPath(baseDir, filepath.Join(p.Dir, p.Manifest))
		for _, r := range raises {
			s.Floors = append(s.Floors, pklFloorSummary{File: file, Name: r.Name, Old: r.Old, New: r.New})
			out.printf("  raised %s %s -> %s in %s\n", r.Name, r.Old, r.New, file)
		}
		if err != nil {
			s.Error = err.Error()
			out.printf("✗ %s (%s): %v\n", s.Dir, p.Tool, err)
			failures++
		}
		s.Changed = p.Fingerprint() != before
		summaries = append(summaries, s)
	}
	return summaries, failures
}

func syncProject(ctx context.Context, p manifest.Project) ([]manifest.Raise, error) {
	raises, err := manifest.Upgrade(ctx, manifest.Exec, p)
	if err != nil {
		return raises, err
	}
	floors, err := p.SyncFloors()
	if err != nil {
		return raises, err
	}
	if len(floors) > 0 {
		if err := manifest.Relock(ctx, manifest.Exec, p); err != nil {
			return append(raises, floors...), err
		}
	}
	return append(raises, floors...), nil
}

func writeManifestSection(body *strings.Builder, manifests []pklManifestSummary) {
	var relocked []string
	var floors []pklFloorSummary
	for _, m := range manifests {
		if !m.Changed {
			continue
		}
		relocked = append(relocked, fmt.Sprintf("`%s` (%s)", m.Dir, m.Tool))
		floors = append(floors, m.Floors...)
	}
	if len(relocked) == 0 {
		return
	}
	sort.SliceStable(floors, func(a, b int) bool { return floors[a].File < floors[b].File })
	body.WriteString("\n### Raised declared floors\n\n")
	fmt.Fprintf(body, "Locks upgraded within their declared ranges by their own package manager, then each declared floor raised to the version now locked: %s.\n", strings.Join(relocked, ", "))
	if len(floors) == 0 {
		return
	}
	body.WriteString("\n| File | Package | Old | New |\n")
	body.WriteString("| --- | --- | --- | --- |\n")
	for _, f := range floors {
		fmt.Fprintf(body, "| `%s` | `%s` | `%s` | `%s` |\n", f.File, f.Name, f.Old, f.New)
	}
}
