package cli

import (
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/kyleking/doneram/internal/lockfile"
	"github.com/kyleking/doneram/internal/osv"
)

// pklLockSummary is one vulnerable package a lockfile resolves: a
// transitive dependency doneram does not pin itself but can raise by
// handing the lockfile back to its package manager.
type pklLockSummary struct {
	File      string           `json:"file"`
	Package   string           `json:"package"`
	Ecosystem string           `json:"ecosystem"`
	Current   string           `json:"current"`
	Fixed     string           `json:"fixed,omitempty"`
	Command   string           `json:"command,omitempty"`
	Updated   bool             `json:"updated"`
	Error     string           `json:"error,omitempty"`
	Vulns     []pklVulnSummary `json:"vulnerabilities"`
	argv      []string
	dir       string
}

// scanLockfiles checks every package every lockfile under baseDir resolves
// against OSV in one batch, and returns the vulnerable ones.
func scanLockfiles(ctx context.Context, baseDir string, client *osv.Client) ([]pklLockSummary, error) {
	files, err := lockfile.Find(baseDir)
	if err != nil {
		return nil, fmt.Errorf("finding lockfiles: %w", err)
	}
	var pkgs []lockfile.Package
	for _, f := range files {
		found, err := lockfile.Parse(f)
		if err != nil {
			return nil, err
		}
		pkgs = append(pkgs, found...)
	}
	if len(pkgs) == 0 {
		return nil, nil
	}

	queries := make([]osv.Query, len(pkgs))
	for i, p := range pkgs {
		queries[i] = osv.Query{Package: p.Name, Ecosystem: p.Ecosystem, Version: p.Version}
	}
	advisories, err := client.Query(ctx, queries)
	if err != nil {
		return nil, err
	}

	var out []pklLockSummary
	for i, p := range pkgs {
		if len(advisories[i]) == 0 {
			continue
		}
		rel, err := filepath.Rel(baseDir, p.File)
		if err != nil {
			rel = p.File
		}
		s := pklLockSummary{
			File:      filepath.ToSlash(rel),
			Package:   p.Name,
			Ecosystem: p.Ecosystem,
			Current:   p.Version,
			Fixed:     osv.MinimumFix(advisories[i], p.Version),
			dir:       filepath.Dir(p.File),
		}
		for _, a := range advisories[i] {
			s.Vulns = append(s.Vulns, pklVulnSummary{ID: a.ID, Severity: a.Severity, URL: a.URL, Fixed: osv.MinimumFix([]osv.Advisory{a}, p.Version)})
		}
		if s.Fixed != "" {
			s.argv = lockfile.FixCommand(p.File, p.Name, s.Fixed)
			s.Command = strings.Join(s.argv, " ")
		}
		out = append(out, s)
	}
	sort.SliceStable(out, func(a, b int) bool {
		if out[a].File != out[b].File {
			return out[a].File < out[b].File
		}
		return out[a].Package < out[b].Package
	})
	return out, nil
}

// applyLockFixes runs each finding's fix command from its lockfile's
// directory. A package manager that is not installed leaves the finding
// reported with the command to run, rather than failing the run.
func applyLockFixes(ctx context.Context, out report, findings []pklLockSummary) (failures int) {
	for i := range findings {
		f := &findings[i]
		if len(f.argv) == 0 {
			continue
		}
		if _, err := exec.LookPath(f.argv[0]); err != nil {
			out.printf("  %s not installed; run `%s` in %s\n", f.argv[0], f.Command, filepath.Dir(f.File))
			continue
		}
		cmd := exec.CommandContext(ctx, f.argv[0], f.argv[1:]...)
		cmd.Dir = f.dir
		if res, err := cmd.CombinedOutput(); err != nil {
			f.Error = fmt.Sprintf("%v: %s", err, strings.TrimSpace(string(res)))
			out.printf("✗ %s %s: `%s` failed: %s\n", f.File, f.Package, f.Command, f.Error)
			failures++
			continue
		}
		f.Updated = true
		out.printf("  raised %s to %s in %s\n", f.Package, f.Fixed, f.File)
	}
	return failures
}

func reportLockFindings(out report, findings []pklLockSummary) {
	if len(findings) == 0 {
		return
	}
	out.printf("\nLockfile advisories:\n")
	for _, f := range findings {
		ids := make([]string, len(f.Vulns))
		for i, v := range f.Vulns {
			ids[i] = v.ID
		}
		fix := "no fixed release"
		if f.Fixed != "" {
			fix = "fixed in " + f.Fixed
		}
		out.printf("⚠ %s: %s %s (%s; %s)\n", f.File, f.Package, f.Current, strings.Join(ids, ", "), fix)
	}
}

func writeLockSection(body *strings.Builder, findings []pklLockSummary) {
	var rows []pklLockSummary
	for _, f := range findings {
		if f.Updated {
			rows = append(rows, f)
		}
	}
	if len(rows) == 0 {
		return
	}
	body.WriteString("\n### Lockfile security fixes\n\n")
	body.WriteString("Transitive dependencies raised to the minimum release that clears their advisories, by the lockfile's own package manager.\n\n")
	body.WriteString("| Lockfile | Package | Current | Fixed | Advisories |\n")
	body.WriteString("| --- | --- | --- | --- | --- |\n")
	for _, r := range rows {
		ids := make([]string, len(r.Vulns))
		for i, v := range r.Vulns {
			ids[i] = v.ID
			if v.URL != "" {
				ids[i] = fmt.Sprintf("[%s](%s)", v.ID, v.URL)
			}
		}
		fmt.Fprintf(body, "| `%s` | `%s` | `%s` | `%s` | %s |\n", r.File, r.Package, r.Current, r.Fixed, strings.Join(ids, ", "))
	}
}
