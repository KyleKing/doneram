package engine

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/kyleking/doneram/internal/locator"
	"github.com/kyleking/doneram/internal/parser"
	"github.com/kyleking/doneram/internal/resolver"
)

type fakeResolver struct {
	version string
	err     error
}

func (f *fakeResolver) Name() string { return "fake" }

func (f *fakeResolver) Resolve(context.Context, string, *parser.VersionPattern) (string, error) {
	return f.version, f.err
}

func (f *fakeResolver) GetChangelog(context.Context, string, string, string) (string, error) {
	return "", nil
}

type detailingResolver struct {
	fakeResolver
	detail string
}

func (d *detailingResolver) Detail(context.Context, string, string, string) (string, error) {
	return d.detail, nil
}

func lookupWith(r resolver.Resolver) ResolverLookup {
	return func(k string) (resolver.Resolver, bool) {
		if k == "mise" {
			return r, true
		}
		return nil, false
	}
}

func TestRunSitesResolvesLatest(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "conf.toml")
	if err := os.WriteFile(path, []byte("jq = \"1.7.1\"\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	sites := []Site{{
		Tool:    "jq",
		Locator: locator.Locator{Glob: path, Pattern: `jq = "([\d.]+)"`, Resolver: "mise"},
	}}

	results := RunSites(context.Background(), sites, lookupWith(&fakeResolver{version: "1.7.2"}))

	if len(results) != 1 {
		t.Fatalf("results = %+v, want 1", results)
	}
	if results[0].Err != nil {
		t.Fatalf("Err = %v", results[0].Err)
	}
	if results[0].Latest != "1.7.2" {
		t.Errorf("Latest = %q, want 1.7.2", results[0].Latest)
	}
}

func TestRunSitesReportsUnavailableResolverWithoutCrashing(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "conf.toml")
	if err := os.WriteFile(path, []byte("jq = \"1.7.1\"\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	sites := []Site{{
		Tool:    "jq",
		Locator: locator.Locator{Glob: path, Pattern: `jq = "([\d.]+)"`, Resolver: "mise"},
	}}

	results := RunSites(context.Background(), sites, func(string) (resolver.Resolver, bool) { return nil, false })

	if len(results) != 1 {
		t.Fatalf("results = %+v, want 1", results)
	}
	if results[0].Err == nil {
		t.Fatal("Err = nil, want a resolver-not-available error")
	}
}

func TestRunSitesCollectsDetailFromDetailer(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "conf.toml")
	if err := os.WriteFile(path, []byte("jq = \"1.7.1\"\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	sites := []Site{{
		Tool:    "jq",
		Locator: locator.Locator{Glob: path, Pattern: `jq = "([\d.]+)"`, Resolver: "mise"},
	}}

	r := &detailingResolver{fakeResolver: fakeResolver{version: "1.7.2"}, detail: "3 commits behind"}
	results := RunSites(context.Background(), sites, lookupWith(r))

	if results[0].Detail != "3 commits behind" {
		t.Errorf("Detail = %q, want %q", results[0].Detail, "3 commits behind")
	}
}

func TestRunSitesCommandSite(t *testing.T) {
	site := Site{
		Tool:           "eslint",
		Command:        `printf 'eslint 8.0.0 9.1.0\nprettier 3.0.0 3.2.0\n'`,
		CommandPattern: `^(?P<name>\S+) (?P<current>\S+) (?P<latest>\S+)$`,
	}

	results := RunSites(context.Background(), []Site{site}, func(string) (resolver.Resolver, bool) { return nil, false })

	if len(results) != 1 {
		t.Fatalf("results = %+v, want 1", results)
	}
	if results[0].Err != nil {
		t.Fatalf("Err = %v", results[0].Err)
	}
	if len(results[0].Matches) != 1 || results[0].Matches[0].Value != "8.0.0" {
		t.Errorf("Matches = %+v, want current 8.0.0", results[0].Matches)
	}
	if results[0].Latest != "9.1.0" {
		t.Errorf("Latest = %q, want 9.1.0", results[0].Latest)
	}
}

func TestRunSitesCommandSitePatchesItsFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "pyproject.toml")
	if err := os.WriteFile(path, []byte("dependencies = [\n  \"uvicorn>=0.52.3\",\n]\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	pattern := `^(?P<name>\S+) v(?P<current>\S+)( \(latest: v(?P<latest>\S+)\))?$`
	sites := []Site{
		{
			Tool:           "uvicorn",
			Command:        `printf 'uvicorn v0.52.3 (latest: v0.52.4)\n'`,
			CommandPattern: pattern,
			Locator:        locator.Locator{Glob: path, Pattern: `uvicorn>=([\d.]+)`},
		},
		{
			Tool:           "anyio",
			Command:        `printf 'anyio v4.14.2\n'`,
			CommandPattern: pattern,
		},
	}

	results := RunSites(context.Background(), sites, func(string) (resolver.Resolver, bool) { return nil, false })

	if results[0].Err != nil {
		t.Fatalf("Err = %v", results[0].Err)
	}
	if len(results[0].Matches) != 1 || results[0].Matches[0].File != path {
		t.Errorf("Matches = %+v, want the pyproject occurrence", results[0].Matches)
	}
	if results[0].Latest != "0.52.4" {
		t.Errorf("Latest = %q, want 0.52.4", results[0].Latest)
	}

	if results[1].Err != nil {
		t.Fatalf("Err = %v", results[1].Err)
	}
	if results[1].Latest != "4.14.2" {
		t.Errorf("Latest = %q, want a package with no newer version to read as current", results[1].Latest)
	}
}

func TestRunSitesCommandSiteNoMatchingEntry(t *testing.T) {
	site := Site{
		Tool:           "eslint",
		Command:        `printf 'prettier 3.0.0 3.2.0\n'`,
		CommandPattern: `^(?P<name>\S+) (?P<current>\S+) (?P<latest>\S+)$`,
	}

	results := RunSites(context.Background(), []Site{site}, func(string) (resolver.Resolver, bool) { return nil, false })

	if results[0].Err == nil {
		t.Fatal("Err = nil, want an error for a missing entry")
	}
}

func TestRunSitesReportsMismatchWithoutCallingResolver(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "conf.toml")
	if err := os.WriteFile(path, []byte("jq = \"1.7.1\"\njq = \"1.7.1\"\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	r := &fakeResolver{version: "9.9.9"}
	sites := []Site{{
		Tool:    "jq",
		Locator: locator.Locator{Glob: path, Pattern: `jq = "([\d.]+)"`, Resolver: "mise", Expect: 1},
	}}

	results := RunSites(context.Background(), sites, lookupWith(r))

	if results[0].Err == nil {
		t.Fatal("Err = nil, want a mismatch error")
	}
	if results[0].Latest != "" {
		t.Errorf("Latest = %q, resolver should not run on a mismatch", results[0].Latest)
	}
}

type countingResolver struct {
	fakeResolver
	mu    sync.Mutex
	calls int
}

func (c *countingResolver) Resolve(ctx context.Context, name string, p *parser.VersionPattern) (string, error) {
	c.mu.Lock()
	c.calls++
	c.mu.Unlock()
	return c.fakeResolver.Resolve(ctx, name, p)
}

func TestRunSitesResolvesEachQuestionOnce(t *testing.T) {
	dir := t.TempDir()
	var sites []Site
	for _, name := range []string{"a.toml", "b.toml", "c.toml"} {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte("jq = \"1.7.1\"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		sites = append(sites, Site{
			Tool:    "jq",
			Locator: locator.Locator{Glob: path, Pattern: `jq = "([\d.]+)"`, Resolver: "mise"},
		})
	}

	r := &countingResolver{fakeResolver: fakeResolver{version: "1.8.2"}}
	results := RunSites(context.Background(), sites, lookupWith(r))

	if r.calls != 1 {
		t.Errorf("resolved %d times, want 1", r.calls)
	}
	for i, got := range results {
		if got.Latest != "1.8.2" {
			t.Errorf("site %d latest = %q, want 1.8.2", i, got.Latest)
		}
	}
}

func TestRunSitesNeverProposesAnOlderVersion(t *testing.T) {
	cases := []struct {
		name       string
		content    string
		pattern    string
		resolved   string
		wantLatest string
		wantBehind string
	}{
		{
			// v0.6.0 and v0.6.1 both inside --min-age, so the newest release
			// outside the window is 0.5.1, older than the pin.
			name:       "cooldown leaves only an older release",
			content:    `doneram@0.6.1#/Config.pkl`,
			pattern:    `doneram@([\d.]+)#`,
			resolved:   "0.5.1",
			wantBehind: "0.5.1",
		},
		{
			name:       "action pin compares its trailing tag",
			content:    `checkout@3d3c42e5aac5ba805825da76410c181273ba90b1 # v7.0.1`,
			pattern:    `checkout@([0-9a-f]{40} # v[\d.]+)`,
			resolved:   "11d5960a326750d5838078e36cf38b85af677262 # v4.4.0",
			wantBehind: "11d5960a326750d5838078e36cf38b85af677262 # v4.4.0",
		},
		{
			name:       "newer version still proposed",
			content:    `jq = "1.7.1"`,
			pattern:    `jq = "([\d.]+)"`,
			resolved:   "1.7.2",
			wantLatest: "1.7.2",
		},
		{
			// alpine3.10 sorts below alpine3.9 as a string; a suffix-only
			// difference must never be read as a downgrade.
			name:       "suffix-only difference is not compared",
			content:    `FROM golang:1.22-alpine3.9`,
			pattern:    `golang:(\S+)`,
			resolved:   "1.22-alpine3.10",
			wantLatest: "1.22-alpine3.10",
		},
		{
			name:       "floating minor pin keeps its precision",
			content:    `version: v2.12`,
			pattern:    `version: v([\d.]+)`,
			resolved:   "2.15.0",
			wantLatest: "2.15",
		},
		{
			name:       "floating minor pin already current",
			content:    `version: v2.15`,
			pattern:    `version: v([\d.]+)`,
			resolved:   "2.15.3",
			wantLatest: "2.15",
		},
		{
			name:       "branch SHA is not compared",
			content:    `action@ffffffffffffffffffffffffffffffffffffffff # master`,
			pattern:    `action@([0-9a-f]{40})`,
			resolved:   "1111111111111111111111111111111111111111",
			wantLatest: "1111111111111111111111111111111111111111",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "pin.txt")
			if err := os.WriteFile(path, []byte(tc.content+"\n"), 0o644); err != nil {
				t.Fatalf("WriteFile: %v", err)
			}
			sites := []Site{{
				Tool:    "tool",
				Locator: locator.Locator{Glob: path, Pattern: tc.pattern, Resolver: "mise"},
			}}

			results := RunSites(context.Background(), sites, lookupWith(&fakeResolver{version: tc.resolved}))

			if results[0].Err != nil {
				t.Fatalf("Err = %v", results[0].Err)
			}
			if results[0].Latest != tc.wantLatest {
				t.Errorf("Latest = %q, want %q", results[0].Latest, tc.wantLatest)
			}
			if results[0].Behind != tc.wantBehind {
				t.Errorf("Behind = %q, want %q", results[0].Behind, tc.wantBehind)
			}
		})
	}
}

func TestMatchPrecision(t *testing.T) {
	cases := []struct{ latest, current, want string }{
		{"2.15.0", "2.12", "2.15"},
		{"v2.15.0", "v2", "v2"},
		{"2.15.0", "2.12.2", "2.15.0"},
		{"1.25", "1.23.4", "1.25"},
		{"1.22-alpine3.10", "1.21", "1.22-alpine3.10"},
		{"abc # v4.4.0", "v4", "abc # v4.4.0"},
	}
	for _, tc := range cases {
		if got := matchPrecision(tc.latest, tc.current); got != tc.want {
			t.Errorf("matchPrecision(%q, %q) = %q, want %q", tc.latest, tc.current, got, tc.want)
		}
	}
}

type yankingResolver struct {
	fakeResolver
	yanked map[string]string
}

func (y *yankingResolver) Yanked(_ context.Context, _ string, v string) (string, bool, error) {
	reason, ok := y.yanked[v]
	return reason, ok, nil
}

func TestRunSitesFlagsYankedPin(t *testing.T) {
	path := filepath.Join(t.TempDir(), "req.txt")
	if err := os.WriteFile(path, []byte("pkg==1.1.0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	sites := []Site{{Tool: "pkg", Locator: locator.Locator{Glob: path, Pattern: `pkg==([\d.]+)`, Resolver: "mise"}}}
	res := &yankingResolver{fakeResolver: fakeResolver{version: "1.0.1"}, yanked: map[string]string{"1.1.0": ""}}

	results := RunSites(context.Background(), sites, lookupWith(res))
	if results[0].Yanked != "yanked" {
		t.Errorf("Yanked = %q, want the default reason", results[0].Yanked)
	}
	if results[0].Latest != "1.0.1" || results[0].Behind != "" {
		t.Errorf("Latest = %q, Behind = %q; a yanked pin should move back to 1.0.1", results[0].Latest, results[0].Behind)
	}
}
