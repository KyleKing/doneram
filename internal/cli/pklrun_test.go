package cli

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kyleking/doneram/internal/eol"
	"github.com/kyleking/doneram/internal/testutil"
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

// eolPkl pins node in a file and names its endoflife.date product. The
// command resolver reports the pin (%[1]s) as current, so only the EOL
// lookup touches the (mock) network.
const eolPkl = `
class Site { file: String; pattern: String }
class Tool {
  resolver: String = "mise"
  sites: Listing<Site> = new {}
  command: String?
  commandPattern: String?
  eol: String?
}

tools: Mapping<String, Tool> = new {
  ["node"] = new {
    command = "printf 'node %[1]s %[1]s\n'"
    commandPattern = #"^(?<name>\S+) (?<current>\S+) (?<latest>\S+)$"#
    eol = "nodejs"
    sites = new { new Site { file = "mise.toml"; pattern = #"node = "([\d.]+)""# } }
  }
}
`

func TestRunCheckPklReportsEOL(t *testing.T) {
	t.Setenv("GITHUB_OUTPUT", "")
	server := testutil.NewMockServer(map[string]http.HandlerFunc{
		"/api/nodejs.json": testutil.FixtureHandler("api/endoflife/nodejs.json"),
	})
	defer server.Close()

	tests := []struct {
		name    string
		pin     string
		want    string
		wantErr bool
	}{
		{name: "cycle found by major", pin: "22.11.0", want: `"date": "2027-04-30"`},
		{name: "no cycle covers the pin", pin: "21.7.3", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, ".doneram.pkl")
			if err := os.WriteFile(path, []byte(fmt.Sprintf(eolPkl, tt.pin)), 0o644); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, "mise.toml"), []byte(`node = "`+tt.pin+`"`+"\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			out := filepath.Join(dir, "summary.json")

			err := runCheckPkl(context.Background(), pklRun{
				path: path, output: out, format: "json", workers: 1,
				eol: eol.NewWithBaseURL(&http.Client{}, server.URL),
			})
			if tt.wantErr {
				if err == nil || !strings.Contains(err.Error(), "unresolved") {
					t.Fatalf("err = %v, want an unresolved site", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("err = %v", err)
			}
			data, err := os.ReadFile(out)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(data), tt.want) || !strings.Contains(string(data), `"cycle": "22"`) {
				t.Errorf("summary = %s, want cycle 22 and %s", data, tt.want)
			}
		})
	}
}
