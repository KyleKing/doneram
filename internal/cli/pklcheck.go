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
	"time"

	"github.com/kyleking/doneram/internal/actionrisk"
	"github.com/kyleking/doneram/internal/config"
	"github.com/kyleking/doneram/internal/discover"
	"github.com/kyleking/doneram/internal/engine"
	"github.com/kyleking/doneram/internal/eol"
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
	Tool    string `json:"tool"`
	Current string `json:"current,omitempty"`
	Latest  string `json:"latest,omitempty"`
	Behind  string `json:"behind,omitempty"`
	// Yanked is the registry's reason the pinned version was withdrawn.
	Yanked          string `json:"yanked,omitempty"`
	Updated         bool   `json:"updated"`
	needsUpdate     bool
	Held            string           `json:"held,omitempty"`
	Detail          string           `json:"detail,omitempty"`
	Compare         string           `json:"compare,omitempty"`
	Error           string           `json:"error,omitempty"`
	Vulnerabilities []pklVulnSummary `json:"vulnerabilities,omitempty"`
	PatchedVersion  string           `json:"patchedVersion,omitempty"`
	HeldVulnerable  bool             `json:"heldVulnerable,omitempty"`
	// Security is set when the proposed version clears the pin's
	// advisories; WaivedMinAge when it was taken ahead of --min-age.
	Security     bool `json:"security,omitempty"`
	WaivedMinAge bool `json:"waivedMinAge,omitempty"`
	// Risk grades a major action bump against how the workflow uses it:
	// "safe", "review", or "breaking". Empty when there is no major bump
	// to judge.
	Risk      string         `json:"risk,omitempty"`
	RiskNotes []string       `json:"riskNotes,omitempty"`
	EOL       *pklEOLSummary `json:"eol,omitempty"`
}

// pklEOLSummary is the end-of-life date of the cycle a site's current pin
// belongs to. Date is empty while endoflife.date publishes none.
type pklEOLSummary struct {
	Product  string `json:"product"`
	Cycle    string `json:"cycle"`
	Date     string `json:"date,omitempty"`
	DaysLeft *int   `json:"daysLeft,omitempty"`
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
	// path is the config, empty for a repo with none, which checks only
	// what discovery finds in the working directory.
	path    string
	apply   bool
	output  string
	workers int
	only    []string
	format  string
	// failOnDrift turns an out-of-date pin into a non-zero exit, for a CI
	// job that goes red rather than opening a pull request.
	failOnDrift bool
	// failOnVuln turns a pin with a known advisory still on disk into a
	// non-zero exit, for a CI gate that blocks on CVEs.
	failOnVuln bool
	// eol overrides the endoflife.date client, nil meaning the public API.
	eol *eol.Client
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
	cfg := &config.Config{}
	baseDir := "."
	if path != "" {
		loaded, err := config.Load(path)
		if err != nil {
			return &ExitError{Code: ExitConfig, Err: fmt.Errorf("loading %s: %w", path, err)}
		}
		cfg = loaded
		baseDir = filepath.Dir(path)
	} else {
		path = "this repo"
	}
	baseDir, err := filepath.Abs(baseDir)
	if err != nil {
		return fmt.Errorf("resolving %s: %w", path, err)
	}

	all := cfg.Sites(baseDir)
	if cfg.DiscoverEnabled() {
		found, err := discover.Sites(baseDir)
		if err != nil {
			return fmt.Errorf("discovering pins: %w", err)
		}
		if all, err = discover.Merge(all, found, cfg.Ignore); err != nil {
			return fmt.Errorf("discovering pins: %w", err)
		}
	}

	sites, err := selectSites(all, run.only)
	if err != nil {
		return &ExitError{Code: ExitConfig, Err: err}
	}
	if len(sites) == 0 {
		out.printf("%s declares no tools and has no discoverable pins\n", path)
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
	security := applySecurityFixes(ctx, results, vulnResults, lookup)
	risks := assessRisks(ctx, results, actionrisk.New(httpClient))

	eolClient := run.eol
	if eolClient == nil {
		eolClient = eol.New(httpClient)
	}
	now := time.Now()

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
		siteSummary.Security = security[i].Fixes
		siteSummary.WaivedMinAge = security[i].Waived
		siteSummary.Risk = string(risks[i].Level)
		siteSummary.RiskNotes = risks[i].Notes
		if repeats >= 0 {
			reportSecurity(out, vuln, security[i])
			reportRisk(out, risks[i])
		}
		if result.Err == nil && result.Site.EOL != "" && siteSummary.Current != "" {
			status, err := eolClient.Lookup(ctx, result.Site.EOL, siteSummary.Current)
			if err != nil {
				out.printf("? %s: %v\n", result.Site.Tool, err)
				siteSummary.Error = err.Error()
				unresolved++
			} else {
				siteSummary.EOL = summarizeEOL(status, now)
				if repeats >= 0 {
					out.printf("    %s\n", eolLine(siteSummary.EOL))
				}
			}
		}
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

	tools := make(map[string]bool, len(sites))
	for _, s := range sites {
		tools[s.Tool] = true
	}
	out.printf("\nChecked %d site(s) across %d tool(s)\n", len(results), len(tools))

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
		code := ExitUnresolved
		switch {
		case patchFailures > 0:
			code = ExitPatch
		case mismatches > 0:
			code = ExitMismatch
		}
		return exitf(code,
			"%d of %d site(s) failed: %d match-count, %d unresolved, %d patch",
			failed, len(results), mismatches, unresolved, patchFailures,
		)
	}

	if run.failOnVuln {
		if n := countVulnerable(summary); n > 0 {
			return exitf(ExitVulnerable, "%d site(s) still pin a version with a known advisory", n)
		}
	}

	if run.failOnDrift && summary.HasUpgrades {
		return exitf(ExitDrift, "%d pin(s) are out of date", countDrift(summary))
	}

	return nil
}

// countVulnerable counts sites whose pin on disk still has an advisory: a
// security fix proposed but not applied leaves the old version in place.
func countVulnerable(summary pklSummary) int {
	var n int
	for _, s := range summary.Results {
		if len(s.Vulnerabilities) > 0 && (!s.Updated || !s.Security) {
			n++
		}
	}
	return n
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
	case result.Behind != "" && len(result.Matches) > 0:
		return fmt.Sprintf("· %s: kept at %s, the newest eligible release (%s) is older", result.Site.Tool, result.Matches[0].Value, result.Behind)
	case result.Latest == "" && len(result.Matches) > 0:
		return fmt.Sprintf("· %s: held at %s, every newer release is inside --min-age", result.Site.Tool, result.Matches[0].Value)
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
	if result.Yanked != "" && len(result.Matches) > 0 {
		out.printf("    ! pinned %s has been withdrawn by the registry: %s\n", result.Matches[0].Value, result.Yanked)
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

// reportSecurity says plainly when a security fix changed what a site was
// moved to, or why it could not.
func reportSecurity(out report, vuln vulncheck.Result, fix securityFix) {
	switch {
	case fix.Err != nil:
		out.printf("    security: could not pin minimum patched %s: %v\n", vuln.PatchedVersion, fix.Err)
	case fix.Waived:
		out.printf("    security: taking %s, the minimum release clearing %s, ahead of --min-age\n", vuln.PatchedVersion, advisoryIDs(vuln))
	}
}

func reportRisk(out report, a actionrisk.Assessment) {
	if a.Level == "" {
		return
	}
	out.printf("    major bump: %s\n", a.Level)
	for _, n := range a.Notes {
		out.printf("      - %s\n", n)
	}
}

func advisoryIDs(vuln vulncheck.Result) string {
	ids := make([]string, 0, len(vuln.Findings))
	for _, f := range vuln.Findings {
		ids = append(ids, f.ID)
	}
	return strings.Join(ids, ", ")
}

func summarizeSite(result engine.SiteResult, vuln vulncheck.Result) pklSiteSummary {
	s := pklSiteSummary{
		Tool:           result.Site.Tool,
		Latest:         result.Latest,
		Behind:         result.Behind,
		Yanked:         result.Yanked,
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

func summarizeEOL(status eol.Status, now time.Time) *pklEOLSummary {
	s := &pklEOLSummary{Product: status.Product, Cycle: status.Cycle}
	if !status.Date.IsZero() {
		days := status.DaysLeft(now)
		s.Date = status.Date.Format(time.DateOnly)
		s.DaysLeft = &days
	}
	return s
}

func eolLine(s *pklEOLSummary) string {
	switch {
	case s.DaysLeft == nil:
		return fmt.Sprintf("eol: %s %s has no published end-of-life date", s.Product, s.Cycle)
	case *s.DaysLeft < 0:
		return fmt.Sprintf("eol: %s %s reached end of life on %s", s.Product, s.Cycle, s.Date)
	default:
		return fmt.Sprintf("eol: %s %s ends %s (%d days)", s.Product, s.Cycle, s.Date, *s.DaysLeft)
	}
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
	// A tool pinned at several sites yields one summary per site; the
	// reviewer needs one row per distinct move, carrying the strongest
	// risk any of its sites reported.
	type key struct{ tool, current, latest string }
	index := map[key]int{}
	var rows []pklSiteSummary
	for _, s := range summary.Results {
		if !s.Updated && !s.needsUpdate {
			continue
		}
		k := key{s.Tool, s.Current, s.Latest}
		if i, ok := index[k]; ok {
			rows[i] = mergeRow(rows[i], s)
			continue
		}
		index[k] = len(rows)
		rows = append(rows, s)
	}

	summary.HasUpgrades = len(rows) > 0
	if !summary.HasUpgrades {
		summary.Title = "chore: no pin updates available"
		summary.Body = "doneram found no pins to update."
		return
	}

	var security, review, routine []pklSiteSummary
	for _, r := range rows {
		switch {
		case r.Security:
			security = append(security, r)
		case r.Risk == string(actionrisk.Breaking) || r.Risk == string(actionrisk.Review):
			review = append(review, r)
		default:
			routine = append(routine, r)
		}
	}

	if len(security) > 0 {
		summary.Title = fmt.Sprintf("fix: update %d pin(s), %d clearing advisories", len(rows), len(security))
	} else {
		summary.Title = fmt.Sprintf("chore: update %d pin(s)", len(rows))
	}

	var body strings.Builder
	body.WriteString("Automated pin updates from doneram.\n")
	writeSection(&body, "Security fixes", "Each moves a pin with a published advisory to a release that clears it. "+
		"Rows marked *ahead of --min-age* take the minimum fixed release before the cooldown would have offered it.", security)
	writeSection(&body, "Needs review", "Major action bumps that change something this repo's workflows use.", review)
	writeSection(&body, "Updates", "", routine)
	summary.Body = body.String()
}

// mergeRow folds another site of the same move into a row, keeping the
// strongest risk and every distinct note.
func mergeRow(row, s pklSiteSummary) pklSiteSummary {
	row.Security = row.Security || s.Security
	row.WaivedMinAge = row.WaivedMinAge || s.WaivedMinAge
	if riskRank(s.Risk) > riskRank(row.Risk) {
		row.Risk = s.Risk
	}
	seen := map[string]bool{}
	for _, n := range row.RiskNotes {
		seen[n] = true
	}
	for _, n := range s.RiskNotes {
		if !seen[n] {
			row.RiskNotes = append(row.RiskNotes, n)
			seen[n] = true
		}
	}
	return row
}

func riskRank(r string) int {
	switch actionrisk.Level(r) {
	case actionrisk.Breaking:
		return 3
	case actionrisk.Review:
		return 2
	case actionrisk.Safe:
		return 1
	}
	return 0
}

func writeSection(body *strings.Builder, title, intro string, rows []pklSiteSummary) {
	if len(rows) == 0 {
		return
	}
	fmt.Fprintf(body, "\n### %s\n\n", title)
	if intro != "" {
		fmt.Fprintf(body, "%s\n\n", intro)
	}
	body.WriteString("| Tool | Current | Latest | Notes | Changes |\n")
	body.WriteString("| --- | --- | --- | --- | --- |\n")
	var details []pklSiteSummary
	for _, r := range rows {
		changes := ""
		if r.Compare != "" {
			changes = fmt.Sprintf("[compare](%s)", r.Compare)
		}
		fmt.Fprintf(body, "| `%s` | `%s` | `%s` | %s | %s |\n", r.Tool, r.Current, r.Latest, rowNote(r), changes)
		if len(r.RiskNotes) > 0 {
			details = append(details, r)
		}
	}
	for _, r := range details {
		fmt.Fprintf(body, "\n<details><summary><code>%s</code> major bump: %s</summary>\n\n", r.Tool, r.Risk)
		for _, n := range r.RiskNotes {
			fmt.Fprintf(body, "- %s\n", n)
		}
		body.WriteString("\n</details>\n")
	}
}

func rowNote(r pklSiteSummary) string {
	var parts []string
	if r.Security {
		ids := make([]string, 0, len(r.Vulnerabilities))
		for _, v := range r.Vulnerabilities {
			ids = append(ids, v.ID)
		}
		parts = append(parts, "fixes "+strings.Join(ids, ", "))
		if r.WaivedMinAge {
			parts = append(parts, "*ahead of --min-age*")
		}
	}
	if r.Yanked != "" {
		parts = append(parts, "current pin withdrawn: "+r.Yanked)
	}
	switch actionrisk.Level(r.Risk) {
	case actionrisk.Safe:
		parts = append(parts, "major, no change to inputs or outputs used here")
	case actionrisk.Review, actionrisk.Breaking:
		parts = append(parts, "major, "+r.Risk)
	}
	return strings.Join(parts, "; ")
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
