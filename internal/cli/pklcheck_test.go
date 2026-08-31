package cli

import (
	"os"
	"path/filepath"
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
