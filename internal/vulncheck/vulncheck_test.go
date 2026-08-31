package vulncheck

import (
	"context"
	"net/http"
	"testing"

	"github.com/kyleking/doneram/internal/engine"
	"github.com/kyleking/doneram/internal/locator"
	"github.com/kyleking/doneram/internal/osv"
	"github.com/kyleking/doneram/internal/parser"
	"github.com/kyleking/doneram/internal/testutil"
	"github.com/kyleking/doneram/internal/vulnscan"
)

func TestCheck_OSV(t *testing.T) {
	server := testutil.NewMockServer(map[string]http.HandlerFunc{
		"/v1/querybatch":                testutil.FixtureHandler("api/osv/querybatch.json"),
		"/v1/vulns/GHSA-9hjg-9r4m-mvj7": testutil.FixtureHandler("api/osv/GHSA-9hjg-9r4m-mvj7.json"),
	})
	defer server.Close()

	client := osv.NewWithBaseURL(&http.Client{}, server.URL)

	results := []engine.SiteResult{
		{
			Site: engine.Site{
				Tool:    "requests",
				Locator: locator.Locator{Resolver: "pypi"},
			},
			Latest:  "2.31.1",
			Matches: []locator.Match{{Value: "2.19.0"}},
		},
	}

	out := Check(context.Background(), results, client, nil)
	if len(out) != 1 {
		t.Fatalf("Check() returned %d results, want 1", len(out))
	}
	if !out[0].Vulnerable() {
		t.Fatal("expected requests 2.19.0 to be vulnerable")
	}

	var found bool
	for _, f := range out[0].Findings {
		if f.ID == "GHSA-9hjg-9r4m-mvj7" {
			found = true
			if f.Severity != "MODERATE" || f.Fixed != "2.32.4" {
				t.Errorf("Finding = %+v, unexpected fields", f)
			}
		}
	}
	if !found {
		t.Fatal("expected GHSA-9hjg-9r4m-mvj7 among findings")
	}
	if out[0].PatchedVersion == "" {
		t.Error("expected a non-empty PatchedVersion")
	}
}

func TestCheck_HeldVulnerable(t *testing.T) {
	server := testutil.NewMockServer(map[string]http.HandlerFunc{
		"/v1/querybatch":                testutil.FixtureHandler("api/osv/querybatch.json"),
		"/v1/vulns/GHSA-9hjg-9r4m-mvj7": testutil.FixtureHandler("api/osv/GHSA-9hjg-9r4m-mvj7.json"),
	})
	defer server.Close()

	client := osv.NewWithBaseURL(&http.Client{}, server.URL)

	held := parser.ParsePattern("#.#.#")
	held.Ceiling = "2.30.0"
	held.HoldReason = "breaking changes"

	results := []engine.SiteResult{
		{
			Site: engine.Site{
				Tool:       "requests",
				Locator:    locator.Locator{Resolver: "pypi"},
				Constraint: held,
			},
			Matches: []locator.Match{{Value: "2.19.0"}},
		},
	}

	out := Check(context.Background(), results, client, nil)
	if !out[0].Vulnerable() {
		t.Fatal("expected requests 2.19.0 to be vulnerable")
	}
	if !out[0].HeldVulnerable {
		t.Error("expected HeldVulnerable, since the 2.32.4 fix sits above the 2.30.0 ceiling")
	}
}

type fakeScanner struct {
	findings []vulnscan.Finding
}

func (f *fakeScanner) Name() string { return "fake" }
func (f *fakeScanner) ScanImage(ctx context.Context, image string) ([]vulnscan.Finding, error) {
	return f.findings, nil
}

func TestCheck_ImageScan(t *testing.T) {
	scanner := &fakeScanner{findings: []vulnscan.Finding{
		{Package: "busybox", Installed: "1.36.1-r15", Fixed: "1.36.1-r17", ID: "CVE-2023-42363", Severity: "HIGH"},
	}}

	results := []engine.SiteResult{
		{
			Site: engine.Site{
				Tool:    "alpine",
				Locator: locator.Locator{Resolver: "dockerhub"},
			},
			Matches: []locator.Match{{Value: "3.19"}},
		},
	}

	out := Check(context.Background(), results, nil, scanner)
	if !out[0].Vulnerable() {
		t.Fatal("expected image scan findings")
	}
	if out[0].Findings[0].Package != "busybox" {
		t.Errorf("Package = %q, want busybox", out[0].Findings[0].Package)
	}
}
