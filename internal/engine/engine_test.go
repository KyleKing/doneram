package engine

import (
	"context"
	"os"
	"path/filepath"
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

func lookupWith(kind string, r resolver.Resolver) ResolverLookup {
	return func(k string) (resolver.Resolver, bool) {
		if k == kind {
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

	results := RunSites(context.Background(), sites, lookupWith("mise", &fakeResolver{version: "1.7.2"}))

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
	results := RunSites(context.Background(), sites, lookupWith("mise", r))

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

	results := RunSites(context.Background(), sites, lookupWith("mise", r))

	if results[0].Err == nil {
		t.Fatal("Err = nil, want a mismatch error")
	}
	if results[0].Latest != "" {
		t.Errorf("Latest = %q, resolver should not run on a mismatch", results[0].Latest)
	}
}
