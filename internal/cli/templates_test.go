package cli

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kyleking/doneram/internal/copier"
	"github.com/kyleking/doneram/internal/engine"
	"github.com/kyleking/doneram/internal/locator"
	"github.com/kyleking/doneram/internal/parser"
)

// stubSource is a template with a workflow and a skip_if_exists README.
type stubSource struct{}

func (stubSource) Files(context.Context, copier.Template) ([]string, []byte, error) {
	return []string{
			"copier.yml",
			"tmpl/.github/workflows/ci.yml.jinja",
			"tmpl/README.md.jinja",
			"tmpl/{{ _copier_conf.answers_file }}.jinja",
		},
		[]byte("_subdirectory: tmpl\n_skip_if_exists:\n  - README.md\n"), nil
}

func TestTemplateOwnership(t *testing.T) {
	dir := t.TempDir()
	for name, body := range map[string]string{
		".copier-answers.yml":       "_commit: v1.2.0\n_src_path: gh:o/tmpl\n",
		".github/workflows/ci.yml":  "x",
		".github/workflows/own.yml": "x",
		"README.md":                 "x",
	} {
		p := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	owned := templateOwnership(context.Background(), report{io.Discard}, dir, stubSource{})
	if len(owned) != 1 {
		t.Fatalf("owned = %v, want only the workflow", owned)
	}
	if src, ok := ownedBy(owned, filepath.Join(dir, ".github/workflows/ci.yml")); !ok || src != "gh:o/tmpl" {
		t.Errorf("ci.yml owned by %q, %v", src, ok)
	}

	sites := []engine.Site{
		{Tool: "checkout", Locator: locator.Locator{Glob: filepath.Join(dir, ".github/workflows/ci.yml")}},
		{Tool: "setup-go", Locator: locator.Locator{Glob: filepath.Join(dir, ".github/workflows/own.yml")}},
	}
	kept := leaveToTemplates(report{io.Discard}, owned, sites)
	if len(kept) != 1 || kept[0].Tool != "setup-go" {
		t.Errorf("kept = %+v, want only the project's own workflow", kept)
	}
}

type stubReleases struct{ latest string }

func (stubReleases) Name() string { return "github-release" }
func (s stubReleases) Resolve(context.Context, string, *parser.VersionPattern) (string, error) {
	return s.latest, nil
}
func (stubReleases) GetChangelog(context.Context, string, string, string) (string, error) {
	return "", nil
}

func TestRunCheckPklCopierTemplate(t *testing.T) {
	t.Setenv("GITHUB_OUTPUT", "")
	dir := t.TempDir()
	cfg := filepath.Join(dir, ".doneram.pkl")
	if err := os.WriteFile(cfg, []byte("tools {}\nlockfiles = false\nmanifests = false\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".copier-answers.yml"), []byte("_commit: v1.2.0\n_src_path: gh:o/tmpl\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	bin := t.TempDir()
	calls := filepath.Join(bin, "calls")
	if err := os.WriteFile(filepath.Join(bin, "copier"), []byte("#!/bin/sh\necho \"$@\" >> "+calls+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))

	// check reports without running copier.
	if err := runCheckPkl(context.Background(), pklRun{path: cfg, format: "json", workers: 1, templateReleases: stubReleases{"1.4.0"}, templateSource: stubSource{}}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(calls); err == nil {
		t.Fatal("check ran copier")
	}

	out := filepath.Join(dir, "summary.json")
	if err := runCheckPkl(context.Background(), pklRun{path: cfg, apply: true, output: out, format: "json", workers: 1, templateReleases: stubReleases{"1.4.0"}, templateSource: stubSource{}}); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(calls)
	if strings.TrimSpace(string(got)) != "update --defaults --skip-answered --vcs-ref v1.4.0 --answers-file .copier-answers.yml" {
		t.Errorf("copier called as %q", got)
	}
	var summary pklSummary
	raw, _ := os.ReadFile(out)
	if err := json.Unmarshal(raw, &summary); err != nil {
		t.Fatal(err)
	}
	if !summary.HasUpgrades || len(summary.Templates) != 1 || !strings.Contains(summary.Body, "Template updates") {
		t.Errorf("summary = %+v", summary)
	}
}
