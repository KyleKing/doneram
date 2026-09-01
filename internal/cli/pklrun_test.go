package cli

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// driftPkl declares one command-backed tool whose command reports a newer
// version than the file on disk, so the run drifts without touching the
// network.
const driftPkl = `
class Site {
  file: String
  pattern: String
  expect: Int(this > 0)?
}

class Tool {
  resolver: String = "mise"
  sites: Listing<Site> = new {}
  command: String?
  commandPattern: String?
}

tools: Mapping<String, Tool> = new {
  ["uvicorn"] = new {
    command = "printf 'uvicorn 0.52.3 0.52.4\n'"
    commandPattern = #"^(?<name>\S+) (?<current>\S+) (?<latest>\S+)$"#
    sites = new {
      new Site {
        file = "pyproject.toml"
        pattern = #"uvicorn>=([\d.]+)"#
      }
    }
  }
}
`

func driftConfig(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, ".doneram.pkl")
	if err := os.WriteFile(path, []byte(driftPkl), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "pyproject.toml"), []byte("uvicorn>=0.52.3\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestRunCheckPklFailOnDrift(t *testing.T) {
	t.Setenv("GITHUB_OUTPUT", "")
	path := driftConfig(t)

	if err := runCheckPkl(context.Background(), pklRun{path: path, format: "json", workers: 2}); err != nil {
		t.Fatalf("drift alone should exit 0: %v", err)
	}

	err := runCheckPkl(context.Background(), pklRun{path: path, format: "json", workers: 2, failOnDrift: true})
	if err == nil {
		t.Fatal("err = nil, want a non-zero exit for an out-of-date pin")
	}
	if !strings.Contains(err.Error(), "out of date") {
		t.Errorf("err = %v, want it to name the drift", err)
	}
}
