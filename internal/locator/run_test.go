package locator

import (
	"context"
	"os"
	"path/filepath"
	"testing"

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
		Locator: Locator{Glob: path, Pattern: `jq = "([\d.]+)"`, Resolver: "mise"},
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
		Locator: Locator{Glob: path, Pattern: `jq = "([\d.]+)"`, Resolver: "mise"},
	}}

	results := RunSites(context.Background(), sites, func(string) (resolver.Resolver, bool) { return nil, false })

	if len(results) != 1 {
		t.Fatalf("results = %+v, want 1", results)
	}
	if results[0].Err == nil {
		t.Fatal("Err = nil, want a resolver-not-available error")
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
		Locator: Locator{Glob: path, Pattern: `jq = "([\d.]+)"`, Resolver: "mise", Expect: 1},
	}}

	results := RunSites(context.Background(), sites, lookupWith("mise", r))

	if results[0].Err == nil {
		t.Fatal("Err = nil, want a mismatch error")
	}
	if results[0].Latest != "" {
		t.Errorf("Latest = %q, resolver should not run on a mismatch", results[0].Latest)
	}
}
