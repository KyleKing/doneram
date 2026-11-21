package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/kyleking/doneram/internal/eol"
	"github.com/kyleking/doneram/internal/parser"
	"github.com/kyleking/doneram/internal/resolver"
	"github.com/kyleking/doneram/internal/support"
)

func TestRunCheckPklLifecycle(t *testing.T) {
	t.Setenv("GITHUB_OUTPUT", "")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/python.json":
			_, _ = w.Write([]byte(`[{"cycle":"3.9","eol":"2025-10-31"},{"cycle":"3.13","eol":"2029-10-31"}]`))
		case "/api/claude.json":
			_, _ = w.Write([]byte(`[{"cycle":"claude-3-opus","eol":"2026-01-05"}]`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	dir := t.TempDir()
	cfg := filepath.Join(dir, ".doneram.pkl")
	if err := os.WriteFile(cfg, []byte("tools {}\nlockfiles = false\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "pyproject.toml"), []byte("[project]\nrequires-python = \">=3.9\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "app.py"), []byte("M = \"claude-3-opus-20240229\"\nN = \"claude-unknown\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	out := filepath.Join(dir, "summary.json")
	err := runCheckPkl(context.Background(), pklRun{
		path: cfg, output: out, format: "json", workers: 1, failOnEOL: true,
		eol: eol.NewWithBaseURL(&http.Client{}, server.URL),
	})
	var exit *ExitError
	if !errors.As(err, &exit) || exit.Code != ExitEOL {
		t.Fatalf("err = %v, want exit %d", err, ExitEOL)
	}
	var summary pklSummary
	data, _ := os.ReadFile(out)
	if err := json.Unmarshal(data, &summary); err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, l := range summary.Lifecycle {
		got = append(got, l.Name+"="+l.Cycle)
	}
	if strings.Join(got, " ") != "claude-3-opus-20240229=claude-3-opus requires-python=3.9" {
		t.Errorf("lifecycle = %v", got)
	}
}

func TestRunCheckPklGitHubAPIVersion(t *testing.T) {
	t.Setenv("GITHUB_OUTPUT", "")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/versions" {
			_, _ = w.Write([]byte(`["2026-03-10"]`))
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()
	githubAPIBase = server.URL
	githubVersionsOnce, githubVersions, githubVersionsErr = sync.Once{}, nil, nil
	t.Cleanup(func() {
		githubAPIBase = "https://api.github.com"
		githubVersionsOnce, githubVersions, githubVersionsErr = sync.Once{}, nil, nil
	})

	dir := t.TempDir()
	cfg := filepath.Join(dir, ".doneram.pkl")
	if err := os.WriteFile(cfg, []byte("tools {}\nlockfiles = false\nmanifests = false\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "gh.py"), []byte("H = {\"X-GitHub-Api-Version\": \"2022-11-28\"}\nOK = {\"X-GitHub-Api-Version\": \"2026-03-10\"}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "summary.json")
	err := runCheckPkl(context.Background(), pklRun{path: cfg, output: out, format: "json", workers: 1, failOnEOL: true, eol: eol.NewWithBaseURL(&http.Client{}, server.URL)})
	var exit *ExitError
	if !errors.As(err, &exit) || exit.Code != ExitEOL {
		t.Fatalf("err = %v, want exit %d", err, ExitEOL)
	}
	data, _ := os.ReadFile(out)
	if !strings.Contains(string(data), "X-GitHub-Api-Version 2022-11-28") || strings.Contains(string(data), "2026-03-10\"") {
		t.Errorf("summary = %s", data)
	}
}

// historyResolver pins at its current version and answers History with a
// project that ships a major a year and patches the last one for a month.
type historyResolver struct{ current string }

func (historyResolver) Name() string { return "github-release" }
func (h historyResolver) Resolve(context.Context, string, *parser.VersionPattern) (string, error) {
	return h.current, nil
}
func (historyResolver) GetChangelog(context.Context, string, string, string) (string, error) {
	return "", nil
}
func (historyResolver) History(context.Context, string) ([]support.Release, error) {
	now := time.Now()
	var out []support.Release
	for major := 1; major <= 6; major++ {
		start := now.AddDate(major-7, 0, 0)
		out = append(out,
			support.Release{Version: fmt.Sprintf("%d.0.0", major), Date: start},
			support.Release{Version: fmt.Sprintf("%d.1.0", major), Date: start.AddDate(0, 6, 0)},
		)
		if major > 1 {
			out = append(out, support.Release{Version: fmt.Sprintf("%d.1.1", major-1), Date: start.AddDate(0, 1, 0)})
		}
	}
	return out, nil
}

func TestRunCheckPklInferredLifecycle(t *testing.T) {
	t.Setenv("GITHUB_OUTPUT", "")
	dir := t.TempDir()
	cfg := filepath.Join(dir, ".doneram.pkl")
	config := `discover = false
lockfiles = false
manifests = false
tools {
  ["widget"] {
    resolver = "github-release"
    resolverName = "acme/widget"
    sites { new { file = "versions.txt"; pattern = #"widget=([\d.]+)"# } }
  }
}
`
	if err := os.WriteFile(cfg, []byte(config), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "versions.txt"), []byte("widget=2.1.1\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	out := filepath.Join(dir, "summary.json")
	err := runCheckPkl(context.Background(), pklRun{
		path: cfg, output: out, format: "json", workers: 1, failOnEOL: true,
		resolvers: map[string]resolver.Resolver{"github-release": historyResolver{"2.1.1"}},
	})
	var exit *ExitError
	if !errors.As(err, &exit) || exit.Code != ExitEOL {
		t.Fatalf("err = %v, want exit %d", err, ExitEOL)
	}
	summary := readSummary(t, out)
	if len(summary.Lifecycle) != 1 || !summary.Lifecycle[0].Inferred || !summary.Lifecycle[0].Past || summary.Lifecycle[0].Name != "widget" {
		t.Fatalf("lifecycle = %+v", summary.Lifecycle)
	}

	// The newest line is maintained, so nothing is reported.
	if err := os.WriteFile(filepath.Join(dir, "versions.txt"), []byte("widget=6.1.0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := runCheckPkl(context.Background(), pklRun{
		path: cfg, output: out, format: "json", workers: 1, failOnEOL: true,
		resolvers: map[string]resolver.Resolver{"github-release": historyResolver{"6.1.0"}},
	}); err != nil {
		t.Fatal(err)
	}
	if s := readSummary(t, out); len(s.Lifecycle) != 0 {
		t.Errorf("lifecycle = %+v", s.Lifecycle)
	}
}
