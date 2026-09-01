package cli

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
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

// findDoneramConfig resolves the config path, preferring an explicit
// --config over the .doneram.pkl in the current directory, which `check`
// itself prefers over the default ./Dockerfile per doneram.md.
func findDoneramConfig(explicit string) (string, bool) {
	name := doneramConfigName
	if explicit != "" {
		name = explicit
	}
	info, err := os.Stat(name)
	if err != nil || info.IsDir() {
		return "", false
	}
	return name, true
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
	Tool            string `json:"tool"`
	Current         string `json:"current,omitempty"`
	Latest          string `json:"latest,omitempty"`
	Updated         bool   `json:"updated"`
	needsUpdate     bool
	Held            string           `json:"held,omitempty"`
	Detail          string           `json:"detail,omitempty"`
	Compare         string           `json:"compare,omitempty"`
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
// pklRun is one invocation of the config path: which config, whether to
// patch, where the summary goes, and how much of it to run. Only names the
// tools to run, empty meaning all of them.
type pklRun struct {
	path    string
	apply   bool
	output  string
	workers int
	only    []string
	format  string
	// failOnDrift turns an out-of-date pin into a non-zero exit, for a CI
	// job that goes red rather than opening a pull request.
	failOnDrift bool
}

// report is the running commentary. The json format keeps stdout clean for
// the summary, so its writes go nowhere.
type report struct{ w io.Writer }

func (r report) printf(format string, a ...any) {
	_, _ = fmt.Fprintf(r.w, format, a...)
}

func (r pklRun) out() report {
	if r.format == "json" {
		return report{io.Discard}
	}
	return report{os.Stdout}
}

func runCheckPkl(ctx context.Context, run pklRun) error {
	path := run.path
	apply := run.apply
	outputPath := run.output
	out := run.out()
	cfg, err := config.Load(path)
	if err != nil {
		return fmt.Errorf("loading %s: %w", path, err)
	}

	baseDir, err := filepath.Abs(filepath.Dir(path))
	if err != nil {
		return fmt.Errorf("resolving %s: %w", path, err)
	}

	sites, err := selectSites(cfg.Sites(baseDir), run.only)
	if err != nil {
		return err
	}
	if len(sites) == 0 {
		out.printf("%s declares no tools\n", path)
		summary := pklSummary{Results: []pklSiteSummary{}}
		finishSummary(&summary)
		return run.emit(summary, outputPath)
	}

	httpClient := httpclient.New(httpclient.DefaultConfig())
	registry := resolver.Registry(httpClient)
	lookup := func(kind string) (resolver.Resolver, bool) {
		r, ok := registry[kind]
		return r, ok
	}

	results := engine.RunSites(ctx, sites, lookup, engine.WithWorkers(run.workers))

	osvClient := osv.New(httpClient)
	scanner, _ := vulnscan.Detect()
	vulnResults := vulncheck.Check(ctx, results, osvClient, scanner)

	var mismatches, unresolved, patchFailures int
	var patchedAny bool
	summary := pklSummary{Results: make([]pklSiteSummary, 0, len(results))}

	headlines := make([]string, len(results))
	for i, result := range results {
		headlines[i] = siteHeadline(result)
	}

	for i, result := range results {
		vuln := vulnResults[i]
		repeats := 0
		if i > 0 && headlines[i-1] == headlines[i] {
			repeats = -1
		} else {
			for j := i + 1; j < len(results) && headlines[j] == headlines[i]; j++ {
				repeats++
			}
		}
		if repeats >= 0 {
			reportSiteResult(out, result, vuln, repeats)
		}

		var mismatch *locator.MismatchError
		switch {
		case errors.As(result.Err, &mismatch):
			mismatches++
		case result.Err != nil:
			unresolved++
		}

		siteSummary := summarizeSite(result, vuln)
		siteSummary.needsUpdate = result.Err == nil && siteNeedsPatch(result)

		if apply && siteSummary.needsUpdate {
			count, err := patchSite(result)
			if err != nil {
				out.printf("✗ %s: patch failed: %v\n", result.Site.Tool, err)
				siteSummary.Error = err.Error()
				patchFailures++
			} else if count > 0 {
				out.printf("  patched %d site(s) for %s\n", count, result.Site.Tool)
				siteSummary.Updated = true
				patchedAny = true
			}
		}

		summary.Results = append(summary.Results, siteSummary)
	}

	out.printf("\nChecked %d site(s) across %d tool(s)\n", len(results), len(cfg.Tools))

	if patchedAny && cfg.AfterPatch != "" {
		if err := runAfterPatch(ctx, out, cfg.AfterPatch, baseDir); err != nil {
			return fmt.Errorf("afterPatch %q: %w", cfg.AfterPatch, err)
		}
	}

	finishSummary(&summary)
	if err := run.emit(summary, outputPath); err != nil {
		return err
	}

	if failed := mismatches + unresolved + patchFailures; failed > 0 {
		return fmt.Errorf(
			"%d of %d site(s) failed: %d match-count, %d unresolved, %d patch",
			failed, len(results), mismatches, unresolved, patchFailures,
		)
	}

	if run.failOnDrift && summary.HasUpgrades {
		return fmt.Errorf("%d pin(s) are out of date", countDrift(summary))
	}

	return nil
}

func countDrift(summary pklSummary) int {
	var n int
	for _, s := range summary.Results {
		if s.Updated || s.needsUpdate {
			n++
		}
	}
	return n
}

// selectSites keeps only the sites whose tool is named in only, and fails
// on a name the config does not declare rather than silently checking
// nothing.
func selectSites(sites []engine.Site, only []string) ([]engine.Site, error) {
	if len(only) == 0 {
		return sites, nil
	}

	wanted := make(map[string]bool, len(only))
	for _, tool := range only {
		wanted[tool] = false
	}

	var kept []engine.Site
	for _, s := range sites {
		if _, ok := wanted[s.Tool]; ok {
			wanted[s.Tool] = true
			kept = append(kept, s)
		}
	}

	var missing []string
	for tool, found := range wanted {
		if !found {
			missing = append(missing, tool)
		}
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		return nil, fmt.Errorf("no site declares %s", strings.Join(missing, ", "))
	}
	return kept, nil
}

// emit writes the JSON summary to stdout under the json format, on top of
// the file and $GITHUB_OUTPUT writes every format performs.
func (r pklRun) emit(summary pklSummary, outputPath string) error {
	if err := writeSummary(summary, outputPath); err != nil {
		return err
	}
	if r.format != "json" {
		return nil
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(summary)
}

// siteHeadline is the one-line verdict for a site. Sites of the same tool
// that reach the same verdict produce the same headline, which is what lets
// the report collapse them.
func siteHeadline(result engine.SiteResult) string {
	var mismatch *locator.MismatchError
	switch {
	case errors.As(result.Err, &mismatch):
		return fmt.Sprintf("✗ %s (%s): %v", result.Site.Tool, result.Site.Locator.Glob, mismatch)
	case result.Err != nil:
		return fmt.Sprintf("? %s (%s): %v", result.Site.Tool, result.Site.Locator.Glob, result.Err)
	case len(result.Matches) > 0 && result.Latest != result.Matches[0].Value:
		return fmt.Sprintf("→ %s: %s -> %s", result.Site.Tool, result.Matches[0].Value, result.Latest)
	default:
		return fmt.Sprintf("✓ %s: up to date (%s)", result.Site.Tool, result.Latest)
	}
}

func reportSiteResult(out report, result engine.SiteResult, vuln vulncheck.Result, repeats int) {
	headline := siteHeadline(result)
	if repeats > 0 {
		out.printf("%s (%d more sites)\n", headline, repeats)
	} else {
		out.printf("%s\n", headline)
	}

	var mismatch *locator.MismatchError
	if errors.As(result.Err, &mismatch) {
		for _, c := range mismatch.Candidates {
			out.printf("    candidate: %s:%d -> %s\n", c.File, c.Line, c.Value)
		}
	}

	if hold := result.Site.Constraint; hold != nil && hold.HoldReason != "" {
		out.printf("    held: %s (ceiling <%s)\n", hold.HoldReason, hold.Ceiling)
	}
	if result.Detail != "" {
		out.printf("    %s\n", result.Detail)
	}

	reportVulnerabilities(out, result, vuln)
}

// reportVulnerabilities prints every advisory found for a site's pin. A
// held pin whose only fix sits above the ceiling is called out explicitly,
// since RunSites already refused to propose that fix as Latest.
func reportVulnerabilities(out report, result engine.SiteResult, vuln vulncheck.Result) {
	if !vuln.Vulnerable() {
		return
	}

	for _, f := range vuln.Findings {
		fixed := f.Fixed
		if fixed == "" {
			fixed = "no known fix"
		}
		out.printf("    ! %s [%s]: %s (fixed: %s) %s\n", f.ID, f.Severity, f.Package, fixed, f.URL)
	}

	switch {
	case vuln.HeldVulnerable:
		out.printf("    held, vulnerable, no fix under the ceiling (minimum patched: %s)\n", vuln.PatchedVersion)
	case vuln.PatchedVersion != "":
		out.printf("    candidates: minimum patched %s, latest matching pattern %s\n", vuln.PatchedVersion, result.Latest)
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
	s.Compare = compareLink(result.Site, s.Current, s.Latest)
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
func runAfterPatch(ctx context.Context, out report, command, baseDir string) error {
	cmd := exec.CommandContext(ctx, "sh", "-c", command)
	cmd.Dir = baseDir
	res, err := cmd.CombinedOutput()
	if len(res) > 0 {
		out.printf("\nafterPatch (%s):\n%s\n", command, res)
	}
	if err != nil {
		return fmt.Errorf("%w: %s", err, strings.TrimSpace(string(res)))
	}
	return nil
}

func finishSummary(summary *pklSummary) {
	var updated []pklSiteSummary
	for _, s := range summary.Results {
		if s.Updated || s.needsUpdate {
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
	body.WriteString("| Tool | Current | Latest | Changes |\n")
	body.WriteString("| --- | --- | --- | --- |\n")
	for _, s := range updated {
		changes := ""
		if s.Compare != "" {
			changes = fmt.Sprintf("[compare](%s)", s.Compare)
		}
		fmt.Fprintf(&body, "| `%s` | `%s` | `%s` | %s |\n", s.Tool, s.Current, s.Latest, changes)
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

	if strings.ContainsAny(summary.Title, "\r\n") {
		return fmt.Errorf("summary title contains a newline: %q", summary.Title)
	}

	if _, err := fmt.Fprintf(f, "has_upgrades=%t\n", summary.HasUpgrades); err != nil {
		return fmt.Errorf("writing GITHUB_OUTPUT: %w", err)
	}
	if err := writeHeredoc(f, "title", summary.Title); err != nil {
		return err
	}
	return writeHeredoc(f, "body", summary.Body)
}

// writeHeredoc emits one $GITHUB_OUTPUT heredoc under a delimiter drawn
// fresh each call, so resolver-supplied text cannot close the block early
// and inject step outputs of its own.
func writeHeredoc(w io.Writer, name, value string) error {
	var buf [16]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return fmt.Errorf("generating output delimiter: %w", err)
	}
	delim := "DONERAM_" + hex.EncodeToString(buf[:])

	if strings.Contains(value, delim) {
		return fmt.Errorf("%s collides with its output delimiter", name)
	}
	if _, err := fmt.Fprintf(w, "%s<<%s\n%s\n%s\n", name, delim, value, delim); err != nil {
		return fmt.Errorf("writing GITHUB_OUTPUT: %w", err)
	}
	return nil
}
