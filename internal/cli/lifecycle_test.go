package cli

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/kyleking/doneram/internal/eol"
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
