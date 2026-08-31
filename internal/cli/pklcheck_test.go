package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
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
