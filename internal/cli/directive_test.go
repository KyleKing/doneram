package cli

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kyleking/doneram/internal/eol"
	"github.com/kyleking/doneram/internal/osv"
	"github.com/kyleking/doneram/internal/resolver"
)

// A Dockerfile directive runs through the same engine as a config site:
// discovered with no config, resolved, patched, and its digest refreshed.
func TestDirectiveRunsThroughEngine(t *testing.T) {
	t.Setenv("GITHUB_OUTPUT", "")
	dir := t.TempDir()
	cfg := filepath.Join(dir, ".doneram.pkl")
	if err := os.WriteFile(cfg, []byte("lockfiles = false\nmanifests = false\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	df := filepath.Join(dir, "Dockerfile")
	if err := os.WriteFile(df, []byte("# doneram: python:3.13.#\nFROM python:3.13.11 AS base\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "query") {
			_, _ = w.Write([]byte(`{"results":[{}]}`))
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	out := filepath.Join(dir, "summary.json")
	run := pklRun{
		path: cfg, apply: true, output: out, format: "json", workers: 1, pin: stubPin{},
		eol:       eol.NewWithBaseURL(server.Client(), server.URL),
		osv:       osv.NewWithBaseURL(server.Client(), server.URL),
		resolvers: map[string]resolver.Resolver{"docker": fixedResolver{"python": "3.13.12"}},
	}
	if err := runCheckPkl(context.Background(), run); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(df)
	if want := "FROM python:3.13.12@sha256:" + strings.Repeat("e", 64) + " AS base"; !strings.Contains(string(data), want) {
		t.Fatalf("Dockerfile = %s, want %s", data, want)
	}
	if s := readSummary(t, out); !strings.Contains(s.Body, "python") {
		t.Errorf("body = %s", s.Body)
	}
}
