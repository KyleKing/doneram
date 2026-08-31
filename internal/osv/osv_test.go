package osv

import (
	"context"
	"net/http"
	"testing"

	"github.com/kyleking/doneram/internal/testutil"
)

func TestClient_Query(t *testing.T) {
	server := testutil.NewMockServer(map[string]http.HandlerFunc{
		"/v1/querybatch":                  testutil.FixtureHandler("api/osv/querybatch.json"),
		"/v1/vulns/GHSA-9hjg-9r4m-mvj7":   testutil.FixtureHandler("api/osv/GHSA-9hjg-9r4m-mvj7.json"),
		"/v1/vulns/ALPINE-CVE-2023-42363": testutil.FixtureHandler("api/osv/ALPINE-CVE-2023-42363.json"),
		"/v1/vulns/DEBIAN-CVE-2023-38545": testutil.FixtureHandler("api/osv/DEBIAN-CVE-2023-38545.json"),
	})
	defer server.Close()

	c := NewWithBaseURL(&http.Client{}, server.URL)
	ctx := context.Background()

	// Order matches the querybatch.json fixture, which was captured from a
	// live request with these five queries in this order.
	queries := []Query{
		{Package: "requests", Ecosystem: "PyPI", Version: "2.19.0"},
		{Package: "lodash", Ecosystem: "npm", Version: "4.17.15"},
		{Package: "golang.org/x/net", Ecosystem: "Go", Version: "0.17.0"},
		{Package: "curl", Ecosystem: "Debian:12", Version: "7.88.1-10"},
		{Package: "busybox", Ecosystem: "Alpine:v3.19", Version: "1.36.1-r15"},
	}

	results, err := c.Query(ctx, queries)
	if err != nil {
		t.Fatalf("Query() error = %v", err)
	}
	if len(results) != len(queries) {
		t.Fatalf("Query() returned %d results, want %d", len(results), len(queries))
	}

	// The batch response carries every advisory OSV knows for these pins
	// (10, 6, and 43 respectively); only the three fetched as detail
	// fixtures show up with full fields, the rest are silently dropped by
	// the "not found in records" check, which is fine for this test's
	// purpose of proving field extraction and ecosystem filtering.
	var found bool
	for _, a := range results[0] {
		if a.ID == "GHSA-9hjg-9r4m-mvj7" {
			found = true
			if a.Severity != "MODERATE" {
				t.Errorf("Severity = %q, want MODERATE", a.Severity)
			}
			if len(a.Fixed) != 1 || a.Fixed[0] != "2.32.4" {
				t.Errorf("Fixed = %v, want [2.32.4]", a.Fixed)
			}
		}
	}
	if !found {
		t.Fatal("expected GHSA-9hjg-9r4m-mvj7 in PyPI results")
	}

	for _, a := range results[4] {
		if a.ID == "ALPINE-CVE-2023-42363" {
			if len(a.Fixed) != 1 || a.Fixed[0] != "1.36.1-r17" {
				t.Errorf("Alpine:v3.19 Fixed = %v, want [1.36.1-r17]", a.Fixed)
			}
		}
	}

	var sawDebian12 bool
	for _, a := range results[3] {
		if a.ID == "DEBIAN-CVE-2023-38545" {
			sawDebian12 = true
			if len(a.Fixed) != 1 || a.Fixed[0] != "7.88.1-10+deb12u4" {
				t.Errorf("Debian:12 Fixed = %v, want [7.88.1-10+deb12u4] (not the Debian:11 fix)", a.Fixed)
			}
		}
	}
	if !sawDebian12 {
		t.Fatal("expected DEBIAN-CVE-2023-38545 in Debian:12 results")
	}
}

func TestMinimumFix(t *testing.T) {
	advisories := []Advisory{
		{ID: "a", Fixed: []string{"1.5.0"}},
		{ID: "b", Fixed: []string{"1.2.0", "1.3.0"}},
	}

	got := MinimumFix(advisories, "1.0.0")
	if got != "1.2.0" {
		t.Errorf("MinimumFix() = %q, want 1.2.0", got)
	}

	if got := MinimumFix(advisories, "2.0.0"); got != "" {
		t.Errorf("MinimumFix() with current above every fix = %q, want empty", got)
	}
}
