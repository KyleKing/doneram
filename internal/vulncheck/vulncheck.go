// Package vulncheck attaches OSV advisories and image-layer scan findings
// to a batch of engine.SiteResult, matching a resolved pin's package,
// ecosystem, and version against known vulnerabilities.
package vulncheck

import (
	"context"
	"strings"

	"github.com/kyleking/doneram/internal/engine"
	"github.com/kyleking/doneram/internal/osv"
	"github.com/kyleking/doneram/internal/vulnscan"
)

// Finding is one vulnerability affecting a site's pin, whether it came from
// OSV (the pin's own package) or an image scan (a package inside a base
// image the pin names).
type Finding struct {
	Package  string
	ID       string
	Severity string
	URL      string
	Fixed    string
}

// Result carries one site's vulnerability findings, plus the minimum
// patched version a vulnerable pin reports alongside its normal Latest
// candidate. The zero Result means nothing was found.
//
// TODO(M6): a version that clears an advisory should waive the minimum
// release age once that system exists; there is no age gate yet to waive.
type Result struct {
	Findings       []Finding
	PatchedVersion string
	HeldVulnerable bool
}

// Vulnerable reports whether any advisory was found for the site.
func (r Result) Vulnerable() bool {
	return len(r.Findings) > 0
}

// imageResolvers names resolver kinds that pin a Docker image tag, whose
// vulnerability data comes from scanning the image's layers rather than
// OSV, which has no way to map a tag to the packages installed inside it.
var imageResolvers = map[string]bool{
	"docker":    true,
	"dockerhub": true,
	"ghcr":      true,
}

// defaultEcosystems maps a resolver kind to its OSV ecosystem name for
// pins where the mapping is unambiguous. Distro packages and Go modules
// need engine.Site.Ecosystem set explicitly, since the resolver kind alone
// doesn't carry the distro release or module path.
var defaultEcosystems = map[string]string{
	"pypi":          "PyPI",
	"npm":           "npm",
	"cargo":         "crates.io",
	"rubygems":      "RubyGems",
	"composer":      "Packagist",
	"github-action": "GitHub Actions",
}

func ecosystemFor(s engine.Site) (string, bool) {
	if s.Ecosystem != "" {
		return s.Ecosystem, true
	}
	eco, ok := defaultEcosystems[s.Locator.Resolver]
	return eco, ok
}

// queryVersion is the version OSV should be asked about. A github-action
// pin is the composite "<sha> # <tag>" the locator patches as one unit, and
// only the tag has an ordering OSV ranges can be read against.
func queryVersion(s engine.Site, pinned string) string {
	if s.Locator.Resolver != "github-action" {
		return pinned
	}
	_, tag, ok := strings.Cut(pinned, "# ")
	if !ok {
		return ""
	}
	return strings.TrimSpace(tag)
}

func packageName(s engine.Site) string {
	if s.ResolverName != "" {
		return s.ResolverName
	}
	return s.Tool
}

// Check returns one Result per entry in results (nil where there is nothing
// to report), querying OSV for every eligible site in a single batched
// request and scanning the image tag of every Docker base-image site.
// Either osvClient or scanner may be nil to skip that source.
func Check(ctx context.Context, results []engine.SiteResult, osvClient *osv.Client, scanner vulnscan.Scanner) []Result {
	out := make([]Result, len(results))

	if osvClient != nil {
		checkOSV(ctx, results, osvClient, out)
	}
	if scanner != nil {
		checkImages(ctx, results, scanner, out)
	}

	return out
}

func checkOSV(ctx context.Context, results []engine.SiteResult, osvClient *osv.Client, out []Result) {
	var queries []osv.Query
	var indexes []int

	for i, r := range results {
		if r.Err != nil || len(r.Matches) == 0 {
			continue
		}
		if imageResolvers[r.Site.Locator.Resolver] {
			continue
		}
		eco, ok := ecosystemFor(r.Site)
		if !ok {
			continue
		}
		pinned := queryVersion(r.Site, r.Matches[0].Value)
		if pinned == "" {
			continue
		}
		queries = append(queries, osv.Query{
			Package:   packageName(r.Site),
			Ecosystem: eco,
			Version:   pinned,
		})
		indexes = append(indexes, i)
	}

	if len(queries) == 0 {
		return
	}

	advisoriesByQuery, err := osvClient.Query(ctx, queries)
	if err != nil {
		return
	}

	for qi, advisories := range advisoriesByQuery {
		if len(advisories) == 0 {
			continue
		}
		i := indexes[qi]
		out[i] = buildOSVResult(results[i], advisories)
	}
}

func buildOSVResult(r engine.SiteResult, advisories []osv.Advisory) Result {
	current := r.Matches[0].Value
	pkg := packageName(r.Site)

	findings := make([]Finding, len(advisories))
	for i, a := range advisories {
		findings[i] = Finding{
			Package:  pkg,
			ID:       a.ID,
			Severity: a.Severity,
			URL:      a.URL,
			Fixed:    osv.MinimumFix([]osv.Advisory{a}, current),
		}
	}

	patched := osv.MinimumFix(advisories, current)
	result := Result{Findings: findings, PatchedVersion: patched}

	if patched != "" {
		if hold := r.Site.Constraint; hold != nil && hold.HoldReason != "" && !hold.Matches(patched) {
			result.HeldVulnerable = true
		}
	}
	return result
}

func checkImages(ctx context.Context, results []engine.SiteResult, scanner vulnscan.Scanner, out []Result) {
	for i, r := range results {
		if r.Err != nil || len(r.Matches) == 0 {
			continue
		}
		if !imageResolvers[r.Site.Locator.Resolver] {
			continue
		}

		image := packageName(r.Site) + ":" + r.Matches[0].Value
		findings, err := scanner.ScanImage(ctx, image)
		if err != nil || len(findings) == 0 {
			continue
		}

		out[i] = Result{Findings: toFindings(findings)}
	}
}

func toFindings(fs []vulnscan.Finding) []Finding {
	out := make([]Finding, len(fs))
	for i, f := range fs {
		out[i] = Finding{Package: f.Package, ID: f.ID, Severity: f.Severity, URL: f.URL, Fixed: f.Fixed}
	}
	return out
}
