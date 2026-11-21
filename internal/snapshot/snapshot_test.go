package snapshot

import (
	"errors"
	"testing"
	"time"

	"github.com/kyleking/doneram/internal/engine"
	"github.com/kyleking/doneram/internal/locator"
)

func site(kind, name, eco string) engine.Site {
	return engine.Site{Tool: name, ResolverName: name, Ecosystem: eco, Locator: locator.Locator{Resolver: kind}}
}

func TestPackageURL(t *testing.T) {
	cases := []struct {
		site      engine.Site
		pinned    string
		wantPURL  string
		wantScope string
	}{
		{site("pypi", "requests", ""), "2.31.0", "pkg:pypi/requests@2.31.0", "runtime"},
		{site("npm", "@types/node", ""), "20.1.0", "pkg:npm/%40types/node@20.1.0", "runtime"},
		{site("github-action", "github/codeql-action", ""), "abc # v3.1.0", "pkg:githubactions/github/codeql-action@v3.1.0", "development"},
		{site("mise", "go:github.com/golangci/golangci-lint/v2/cmd/golangci-lint", ""), "2.12.2", "pkg:golang/github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.12.2", "development"},
		{site("ghcr", "kyleking/doneram", ""), "1.2.3", "pkg:docker/ghcr.io/kyleking/doneram@1.2.3", "runtime"},
		{site("github-release", "kyleking/doneram", ""), "0.6.1", "pkg:github/kyleking/doneram@0.6.1", "runtime"},
		{site("apk", "busybox", "Alpine:v3.19"), "1.36.1-r15", "", ""},
		{site("mise", "jq", ""), "1.7.1", "", ""},
		{site("mise", "go:example.com/tool", ""), "2.12", "", ""},
		{site("github-action", "actions/checkout", ""), "abc", "", ""},
	}
	for _, tc := range cases {
		purl, scope, ok := PackageURL(tc.site, tc.pinned)
		if purl != tc.wantPURL || scope != tc.wantScope || ok != (tc.wantPURL != "") {
			t.Errorf("PackageURL(%s %s, %q) = %q, %q, %v; want %q, %q", tc.site.Locator.Resolver, tc.site.Tool, tc.pinned, purl, scope, ok, tc.wantPURL, tc.wantScope)
		}
	}
}

func TestBuild(t *testing.T) {
	results := []engine.SiteResult{
		{Site: site("github-action", "actions/checkout", ""), Matches: []locator.Match{
			{File: "/repo/.github/workflows/ci.yml", Value: "a # v4.4.0"},
			{File: "/repo/.github/workflows/ci.yml", Value: "a # v4.4.0"},
			{File: "/repo/.github/workflows/release.yml", Value: "b # v4.3.0"},
		}},
		{Site: site("pypi", "requests", ""), Err: errors.New("boom"), Matches: []locator.Match{{File: "/repo/req.txt", Value: "1"}}},
	}
	s := Build(results, Options{Root: "/repo", SHA: "deadbeef", Ref: "refs/heads/main", Version: "1.0.0", Scanned: time.Unix(0, 0)})

	if got := s.Files(); len(got) != 3 || got[0] != ".github/workflows/ci.yml" || got[2] != "req.txt" {
		t.Fatalf("Files = %v", got)
	}
	ci := s.Manifest[".github/workflows/ci.yml"]
	if len(ci.Resolved) != 1 || ci.Resolved["pkg:githubactions/actions/checkout@v4.4.0"].Relationship != "direct" {
		t.Errorf("ci manifest = %+v", ci)
	}
	if s.Job.ID == "" || s.Scanned != "1970-01-01T00:00:00Z" || s.Detector.Version != "1.0.0" {
		t.Errorf("snapshot header = %+v", s)
	}
}
