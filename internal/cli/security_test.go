package cli

import (
	"context"
	"strings"
	"testing"

	"github.com/kyleking/doneram/internal/engine"
	"github.com/kyleking/doneram/internal/locator"
	"github.com/kyleking/doneram/internal/parser"
	"github.com/kyleking/doneram/internal/resolver"
	"github.com/kyleking/doneram/internal/vulncheck"
)

type pinningResolver struct{ pinned []string }

func (p *pinningResolver) Name() string { return "github-action" }
func (p *pinningResolver) Resolve(context.Context, string, *parser.VersionPattern) (string, error) {
	return "", nil
}
func (p *pinningResolver) GetChangelog(context.Context, string, string, string) (string, error) {
	return "", nil
}
func (p *pinningResolver) Pin(_ context.Context, _ string, version string) (string, error) {
	p.pinned = append(p.pinned, version)
	return "abc # v" + version, nil
}

func securitySite(resolverKind, current, latest string, constraint *parser.VersionPattern) engine.SiteResult {
	return engine.SiteResult{
		Site: engine.Site{
			Tool:         "tool",
			ResolverName: "owner/repo",
			Constraint:   constraint,
			Locator:      locator.Locator{Glob: "f", Resolver: resolverKind},
		},
		Matches: []locator.Match{{File: "f", Line: 1, Value: current}},
		Latest:  latest,
	}
}

func TestApplySecurityFixes(t *testing.T) {
	pinner := &pinningResolver{}
	lookup := func(kind string) (resolver.Resolver, bool) {
		if kind == "github-action" {
			return pinner, true
		}
		return nil, false
	}
	vuln := vulncheck.Result{Findings: []vulncheck.Finding{{ID: "GHSA-x"}}, PatchedVersion: "46.0.1"}

	cases := []struct {
		name       string
		result     engine.SiteResult
		vuln       vulncheck.Result
		wantLatest string
		want       securityFix
	}{
		{
			name:       "cooldown held every release: take the minimum fix",
			result:     securitySite("github-action", "sha # v45.0.7", "", nil),
			vuln:       vuln,
			wantLatest: "abc # v46.0.1",
			want:       securityFix{Fixes: true, Waived: true},
		},
		{
			name:       "normal update already clears it",
			result:     securitySite("github-action", "sha # v45.0.7", "def # v47.0.0", nil),
			vuln:       vuln,
			wantLatest: "def # v47.0.0",
			want:       securityFix{Fixes: true},
		},
		{
			name:       "eligible update still vulnerable",
			result:     securitySite("pypi", "v45.0.7", "45.0.9", nil),
			vuln:       vuln,
			wantLatest: "v46.0.1",
			want:       securityFix{Fixes: true, Waived: true},
		},
		{
			name:       "hold ceiling below the fix is never crossed",
			result:     securitySite("pypi", "45.0.7", "", &parser.VersionPattern{Ceiling: "46.0.0", HoldReason: "breaks"}),
			vuln:       vulncheck.Result{Findings: vuln.Findings, PatchedVersion: "46.0.1", HeldVulnerable: true},
			wantLatest: "",
		},
		{
			name:       "not vulnerable",
			result:     securitySite("pypi", "1.0.0", "1.0.1", nil),
			wantLatest: "1.0.1",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			results := []engine.SiteResult{tc.result}
			fixes := applySecurityFixes(context.Background(), results, []vulncheck.Result{tc.vuln}, lookup)
			if results[0].Latest != tc.wantLatest {
				t.Errorf("Latest = %q, want %q", results[0].Latest, tc.wantLatest)
			}
			if fixes[0] != tc.want {
				t.Errorf("fix = %+v, want %+v", fixes[0], tc.want)
			}
		})
	}
}

func TestFinishSummaryGroupsByRisk(t *testing.T) {
	summary := pklSummary{Results: []pklSiteSummary{
		{Tool: "checkout", Current: "a # v4.4.0", Latest: "b # v7.0.1", needsUpdate: true, Risk: "safe", RiskNotes: []string{"runtime node20 → node24"}},
		{Tool: "checkout", Current: "a # v4.4.0", Latest: "b # v7.0.1", needsUpdate: true, Risk: "breaking", RiskNotes: []string{"input `x` was removed"}},
		{Tool: "changed-files", Current: "c # v45.0.7", Latest: "d # v46.0.1", needsUpdate: true, Security: true, WaivedMinAge: true,
			Vulnerabilities: []pklVulnSummary{{ID: "GHSA-mrrh-fwg8-r2c3"}}},
		{Tool: "jq", Current: "1.7.1", Latest: "1.7.2", needsUpdate: true},
		{Tool: "pkl", Current: "0.32.1", Latest: "0.32.1"},
	}}
	finishSummary(&summary)

	if summary.Title != "fix: update 3 pin(s), 1 clearing advisories" {
		t.Errorf("Title = %q", summary.Title)
	}
	body := summary.Body
	for _, want := range []string{
		"### Security fixes", "fixes GHSA-mrrh-fwg8-r2c3; *ahead of --min-age*",
		"### Needs review", "major, breaking", "input `x` was removed", "runtime node20 → node24",
		"### Updates", "| `jq` | `1.7.1` | `1.7.2` |",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("body missing %q:\n%s", want, body)
		}
	}
	if strings.Count(body, "| `checkout` |") != 1 {
		t.Errorf("checkout should collapse to one row:\n%s", body)
	}
	if strings.Contains(body, "`pkl`") {
		t.Errorf("an up-to-date pin should not be listed:\n%s", body)
	}
}
