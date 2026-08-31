package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kyleking/doneram/internal/engine"
	"github.com/kyleking/doneram/internal/locator"
	"github.com/kyleking/doneram/internal/parser"
	"github.com/kyleking/doneram/internal/vulncheck"
)

const doneramPklFixture = `
class Site { file: String; pattern: String; expect: Int(this > 0) = 1 }
class Tool { resolver: String = "mise"; resolverName: String?; sites: Listing<Site> }

tools: Mapping<String, Tool> = new {
  ["jq"] = new {
    sites = new { new Site { file = "conf.toml"; pattern = #"jq = "([\d.]+)""# } }
  }
}
`

func TestRunCheckPklReportsUnavailableResolverWithoutCrashing(t *testing.T) {
	dir := t.TempDir()
	chdir(t, dir)

	if err := os.WriteFile(filepath.Join(dir, ".doneram.pkl"), []byte(doneramPklFixture), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "conf.toml"), []byte("jq = \"1.7.1\"\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	if err := runApp(t, "check"); err != nil {
		t.Fatalf("check: %v", err)
	}
}

func TestRunCheckFallsBackToDockerfileWithoutConfig(t *testing.T) {
	dir := t.TempDir()
	chdir(t, dir)
	writeDockerfile(t, dir, "Dockerfile")

	if err := runApp(t, "check"); err != nil {
		t.Fatalf("check: %v", err)
	}
}

const doneramPklFixtureWithAfterPatch = `
afterPatch = "./after.sh"
class Site { file: String; pattern: String; expect: Int(this > 0) = 1 }
class Tool { resolver: String = "mise"; resolverName: String?; sites: Listing<Site> }

tools: Mapping<String, Tool> = new {
  ["jq"] = new {
    sites = new { new Site { file = "conf.toml"; pattern = #"jq = "([\d.]+)""# } }
  }
}
`

func TestRunCheckPklApplyPatchesAndRunsAfterPatch(t *testing.T) {
	dir := t.TempDir()
	chdir(t, dir)

	if err := os.WriteFile(filepath.Join(dir, ".doneram.pkl"), []byte(doneramPklFixtureWithAfterPatch), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "conf.toml"), []byte("jq = \"1.0.0\"\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	afterScript := "#!/bin/sh\ntouch after-ran\n"
	if err := os.WriteFile(filepath.Join(dir, "after.sh"), []byte(afterScript), 0o755); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	outputPath := filepath.Join(dir, "summary.json")
	if err := runApp(t, "check", "--apply", "--output", outputPath); err != nil {
		t.Fatalf("check --apply: %v", err)
	}

	patched, err := os.ReadFile(filepath.Join(dir, "conf.toml"))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if strings.Contains(string(patched), "1.0.0") {
		t.Errorf("conf.toml still pins 1.0.0 after --apply: %s", patched)
	}

	if _, err := os.Stat(filepath.Join(dir, "after-ran")); err != nil {
		t.Errorf("afterPatch did not run: %v", err)
	}

	summary, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("ReadFile(summary): %v", err)
	}
	if !strings.Contains(string(summary), `"has_upgrades": true`) {
		t.Errorf("summary = %s, want has_upgrades: true", summary)
	}
}

func TestRunCheckPklReportsUpgradesWithoutApplying(t *testing.T) {
	dir := t.TempDir()
	chdir(t, dir)

	if err := os.WriteFile(filepath.Join(dir, ".doneram.pkl"), []byte(doneramPklFixtureWithAfterPatch), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "conf.toml"), []byte("jq = \"1.0.0\"\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	outputPath := filepath.Join(dir, "summary.json")
	if err := runApp(t, "check", "--output", outputPath); err != nil {
		t.Fatalf("check: %v", err)
	}

	unpatched, err := os.ReadFile(filepath.Join(dir, "conf.toml"))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if !strings.Contains(string(unpatched), "1.0.0") {
		t.Errorf("conf.toml was patched without --apply: %s", unpatched)
	}

	summary, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("ReadFile(summary): %v", err)
	}
	if !strings.Contains(string(summary), `"has_upgrades": true`) {
		t.Errorf("summary = %s, want has_upgrades: true", summary)
	}
}

func TestRunCheckPklFailsOnMatchCountMismatch(t *testing.T) {
	dir := t.TempDir()
	chdir(t, dir)

	if err := os.WriteFile(filepath.Join(dir, ".doneram.pkl"), []byte(doneramPklFixture), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "conf.toml"), []byte("jq = \"v1.7.1\"\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	if err := runApp(t, "check"); err == nil {
		t.Error("check should fail when a site's match count disagrees with expect")
	}
}

func TestSummarizeSite_Vulnerabilities(t *testing.T) {
	held := parser.ParsePattern("#.#.#")
	held.Ceiling = "2.30.0"
	held.HoldReason = "breaking changes"

	result := engine.SiteResult{
		Site: engine.Site{
			Tool:       "requests",
			Locator:    locator.Locator{Resolver: "pypi"},
			Constraint: held,
		},
		Matches: []locator.Match{{Value: "2.19.0"}},
		Latest:  "2.30.0",
	}
	vuln := vulncheck.Result{
		Findings: []vulncheck.Finding{
			{Package: "requests", ID: "GHSA-9hjg-9r4m-mvj7", Severity: "MODERATE", URL: "https://osv.dev/vulnerability/GHSA-9hjg-9r4m-mvj7", Fixed: "2.32.4"},
		},
		PatchedVersion: "2.32.4",
		HeldVulnerable: true,
	}

	summary := summarizeSite(result, vuln)

	if !summary.HeldVulnerable {
		t.Error("HeldVulnerable = false, want true")
	}
	if summary.PatchedVersion != "2.32.4" {
		t.Errorf("PatchedVersion = %q, want 2.32.4", summary.PatchedVersion)
	}
	if len(summary.Vulnerabilities) != 1 || summary.Vulnerabilities[0].ID != "GHSA-9hjg-9r4m-mvj7" {
		t.Errorf("Vulnerabilities = %+v, want one finding for GHSA-9hjg-9r4m-mvj7", summary.Vulnerabilities)
	}
}

func TestSummarizeSite_NoVulnerabilitiesLeavesFieldsEmpty(t *testing.T) {
	result := engine.SiteResult{
		Site:    engine.Site{Tool: "jq"},
		Matches: []locator.Match{{Value: "1.7.1"}},
		Latest:  "1.7.1",
	}

	summary := summarizeSite(result, vulncheck.Result{})

	if summary.Vulnerabilities != nil || summary.PatchedVersion != "" || summary.HeldVulnerable {
		t.Errorf("summary = %+v, want zero-value vulnerability fields for a non-vulnerable site", summary)
	}
}
