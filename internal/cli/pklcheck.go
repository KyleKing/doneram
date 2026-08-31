package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/kyleking/doneram/internal/config"
	"github.com/kyleking/doneram/internal/engine"
	"github.com/kyleking/doneram/internal/httpclient"
	"github.com/kyleking/doneram/internal/locator"
	"github.com/kyleking/doneram/internal/osv"
	"github.com/kyleking/doneram/internal/resolver"
	"github.com/kyleking/doneram/internal/vulncheck"
	"github.com/kyleking/doneram/internal/vulnscan"
)

const doneramConfigName = ".doneram.pkl"

// findDoneramConfig looks for a .doneram.pkl in the current directory,
// which `check` prefers over the default ./Dockerfile per doneram.md.
func findDoneramConfig() (string, bool) {
	info, err := os.Stat(doneramConfigName)
	if err != nil || info.IsDir() {
		return "", false
	}
	return doneramConfigName, true
}

// pklSummary is the JSON summary contract a scheduled workflow reads: enough
// to decide whether to open a PR and to write its title and body.
type pklSummary struct {
	HasUpgrades bool             `json:"has_upgrades"`
	Title       string           `json:"title"`
	Body        string           `json:"body"`
	Results     []pklSiteSummary `json:"results"`
}

type pklSiteSummary struct {
	Tool            string           `json:"tool"`
	Current         string           `json:"current,omitempty"`
	Latest          string           `json:"latest,omitempty"`
	Updated         bool             `json:"updated"`
	Held            string           `json:"held,omitempty"`
	Detail          string           `json:"detail,omitempty"`
	Error           string           `json:"error,omitempty"`
	Vulnerabilities []pklVulnSummary `json:"vulnerabilities,omitempty"`
	PatchedVersion  string           `json:"patchedVersion,omitempty"`
	HeldVulnerable  bool             `json:"heldVulnerable,omitempty"`
}

// pklVulnSummary is one advisory found for a site's pin, whether from OSV
// or an image layer scan.
type pklVulnSummary struct {
	Package  string `json:"package,omitempty"`
	ID       string `json:"id"`
	Severity string `json:"severity,omitempty"`
	URL      string `json:"url,omitempty"`
	Fixed    string `json:"fixed,omitempty"`
}

// runCheckPkl loads a repo's .doneram.pkl and reports on every site it
// declares, routing each through the same locator/resolver engine a
// Dockerfile directive compiles into. With apply set, a site whose latest
// version disagrees with what's on disk is patched in place and afterPatch
// runs once if anything changed.
func runCheckPkl(ctx context.Context, path string, apply bool, outputPath string) error {
	cfg, err := config.Load(path)
	if err != nil {
		return fmt.Errorf("loading %s: %w", path, err)
	}

	baseDir, err := filepath.Abs(filepath.Dir(path))
	if err != nil {
		return fmt.Errorf("resolving %s: %w", path, err)
	}

	sites := cfg.Sites(baseDir)
	if len(sites) == 0 {
		fmt.Printf("%s declares no tools\n", path)
		return nil
	}

	httpClient := httpclient.New(httpclient.DefaultConfig())
	registry := resolver.Registry(httpClient)
	lookup := func(kind string) (resolver.Resolver, bool) {
		r, ok := registry[kind]
		return r, ok
	}

	results := engine.RunSites(ctx, sites, lookup)

	osvClient := osv.New(httpClient)
	scanner, _ := vulnscan.Detect()
	vulnResults := vulncheck.Check(ctx, results, osvClient, scanner)

	var mismatches int
	var patchedAny bool
	summary := pklSummary{Results: make([]pklSiteSummary, 0, len(results))}

	for i, result := range results {
		vuln := vulnResults[i]
		reportSiteResult(result, vuln)

		var mismatch *locator.MismatchError
		if errors.As(result.Err, &mismatch) {
			mismatches++
		}

		siteSummary := summarizeSite(result, vuln)

		if apply && result.Err == nil && siteNeedsPatch(result) {
			count, err := patchSite(result)
			if err != nil {
				fmt.Printf("✗ %s: patch failed: %v\n", result.Site.Tool, err)
				siteSummary.Error = err.Error()
			} else if count > 0 {
				fmt.Printf("  patched %d site(s) for %s\n", count, result.Site.Tool)
				siteSummary.Updated = true
				patchedAny = true
			}
		}

		summary.Results = append(summary.Results, siteSummary)
	}

	fmt.Printf("\nChecked %d site(s) across %d tool(s)\n", len(results), len(cfg.Tools))

	if patchedAny && cfg.AfterPatch != "" {
		if err := runAfterPatch(ctx, cfg.AfterPatch, baseDir); err != nil {
			return fmt.Errorf("afterPatch %q: %w", cfg.AfterPatch, err)
		}
	}

	finishSummary(&summary)
	if err := writeSummary(summary, outputPath); err != nil {
		return err
	}

	if mismatches > 0 {
		return fmt.Errorf("%d site(s) failed match-count validation", mismatches)
	}

	return nil
}

func reportSiteResult(result engine.SiteResult, vuln vulncheck.Result) {
	var mismatch *locator.MismatchError
	switch {
	case errors.As(result.Err, &mismatch):
		fmt.Printf("✗ %s (%s): %v\n", result.Site.Tool, result.Site.Locator.Glob, mismatch)
		for _, c := range mismatch.Candidates {
			fmt.Printf("    candidate: %s:%d -> %s\n", c.File, c.Line, c.Value)
		}
	case result.Err != nil:
		fmt.Printf("? %s (%s): %v\n", result.Site.Tool, result.Site.Locator.Glob, result.Err)
	case len(result.Matches) > 0 && result.Latest != result.Matches[0].Value:
		fmt.Printf("→ %s: %s -> %s\n", result.Site.Tool, result.Matches[0].Value, result.Latest)
	default:
		fmt.Printf("✓ %s: up to date (%s)\n", result.Site.Tool, result.Latest)
	}

	if hold := result.Site.Constraint; hold != nil && hold.HoldReason != "" {
		fmt.Printf("    held: %s (ceiling <%s)\n", hold.HoldReason, hold.Ceiling)
	}
	if result.Detail != "" {
		fmt.Printf("    %s\n", result.Detail)
	}

	reportVulnerabilities(result, vuln)
}

// reportVulnerabilities prints every advisory found for a site's pin. A
// held pin whose only fix sits above the ceiling is called out explicitly,
// since RunSites already refused to propose that fix as Latest.
func reportVulnerabilities(result engine.SiteResult, vuln vulncheck.Result) {
	if !vuln.Vulnerable() {
		return
	}

	for _, f := range vuln.Findings {
		fixed := f.Fixed
		if fixed == "" {
			fixed = "no known fix"
		}
		fmt.Printf("    ! %s [%s]: %s (fixed: %s) %s\n", f.ID, f.Severity, f.Package, fixed, f.URL)
	}

	switch {
	case vuln.HeldVulnerable:
		fmt.Printf("    held, vulnerable, no fix under the ceiling (minimum patched: %s)\n", vuln.PatchedVersion)
	case vuln.PatchedVersion != "":
		fmt.Printf("    candidates: minimum patched %s, latest matching pattern %s\n", vuln.PatchedVersion, result.Latest)
	}
}

func summarizeSite(result engine.SiteResult, vuln vulncheck.Result) pklSiteSummary {
	s := pklSiteSummary{
		Tool:           result.Site.Tool,
		Latest:         result.Latest,
		Detail:         result.Detail,
		PatchedVersion: vuln.PatchedVersion,
		HeldVulnerable: vuln.HeldVulnerable,
	}
	if len(result.Matches) > 0 {
		s.Current = result.Matches[0].Value
	}
	if result.Err != nil {
		s.Error = result.Err.Error()
	}
	for _, f := range vuln.Findings {
		s.Vulnerabilities = append(s.Vulnerabilities, pklVulnSummary{
			Package:  f.Package,
			ID:       f.ID,
			Severity: f.Severity,
			URL:      f.URL,
			Fixed:    f.Fixed,
		})
	}
	if hold := result.Site.Constraint; hold != nil && hold.HoldReason != "" {
		s.Held = hold.HoldReason
	}
	return s
}

// siteNeedsPatch reports whether result found a site whose current value on
// disk disagrees with the resolved latest version. Command sites have
// nothing on disk to patch.
func siteNeedsPatch(result engine.SiteResult) bool {
	if result.Site.Locator.Glob == "" {
		return false
	}
	if result.Latest == "" || len(result.Matches) == 0 {
		return false
	}
	return result.Matches[0].Value != result.Latest
}

// patchSite rewrites every file result.Matches touched, since a site's glob
// may cover more than one file, and returns the total occurrences patched.
func patchSite(result engine.SiteResult) (int, error) {
	seen := make(map[string]bool)
	var total int
	for _, m := range result.Matches {
		if seen[m.File] {
			continue
		}
		seen[m.File] = true

		count, err := locator.Patch(m.File, result.Site.Locator.Pattern, result.Latest)
		if err != nil {
			return total, fmt.Errorf("patching %s: %w", m.File, err)
		}
		total += count
	}
	return total, nil
}

// runAfterPatch runs a repo's afterPatch command from baseDir, the
// directory holding .doneram.pkl, so a relative script path resolves the
// way it does when a developer runs it by hand.
func runAfterPatch(ctx context.Context, command, baseDir string) error {
	cmd := exec.CommandContext(ctx, "sh", "-c", command)
	cmd.Dir = baseDir
	out, err := cmd.CombinedOutput()
	if len(out) > 0 {
		fmt.Printf("\nafterPatch (%s):\n%s\n", command, out)
	}
	if err != nil {
		return fmt.Errorf("%w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func finishSummary(summary *pklSummary) {
	var updated []pklSiteSummary
	for _, s := range summary.Results {
		if s.Updated {
			updated = append(updated, s)
		}
	}

	summary.HasUpgrades = len(updated) > 0
	if !summary.HasUpgrades {
		summary.Title = "chore: no pin updates available"
		summary.Body = "doneram found no pins to update."
		return
	}

	summary.Title = fmt.Sprintf("chore: update %d pin(s)", len(updated))

	var body strings.Builder
	body.WriteString("Automated pin updates from doneram.\n\n")
	body.WriteString("| Tool | Current | Latest |\n")
	body.WriteString("| --- | --- | --- |\n")
	for _, s := range updated {
		fmt.Fprintf(&body, "| `%s` | `%s` | `%s` |\n", s.Tool, s.Current, s.Latest)
	}
	summary.Body = body.String()
}

// writeSummary writes summary as JSON to outputPath when set, and always
// mirrors has_upgrades, title, and body to $GITHUB_OUTPUT when running in a
// GitHub Actions job, matching the contract the scheduled workflow reads.
func writeSummary(summary pklSummary, outputPath string) error {
	if outputPath != "" {
		data, err := json.MarshalIndent(summary, "", "  ")
		if err != nil {
			return fmt.Errorf("marshaling summary: %w", err)
		}
		if err := os.WriteFile(outputPath, data, 0o644); err != nil {
			return fmt.Errorf("writing %s: %w", outputPath, err)
		}
	}

	outputsFile := os.Getenv("GITHUB_OUTPUT")
	if outputsFile == "" {
		return nil
	}

	f, err := os.OpenFile(outputsFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("opening GITHUB_OUTPUT: %w", err)
	}
	defer func() { _ = f.Close() }()

	fmt.Fprintf(f, "has_upgrades=%t\n", summary.HasUpgrades)
	fmt.Fprintf(f, "title<<DONERAM_EOF\n%s\nDONERAM_EOF\n", summary.Title)
	fmt.Fprintf(f, "body<<DONERAM_EOF\n%s\nDONERAM_EOF\n", summary.Body)

	return nil
}
