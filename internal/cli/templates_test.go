package cli

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kyleking/doneram/internal/parser"
)

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
	if err := runCheckPkl(context.Background(), pklRun{path: cfg, format: "json", workers: 1, templateReleases: stubReleases{"1.4.0"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(calls); err == nil {
		t.Fatal("check ran copier")
	}

	out := filepath.Join(dir, "summary.json")
	if err := runCheckPkl(context.Background(), pklRun{path: cfg, apply: true, output: out, format: "json", workers: 1, templateReleases: stubReleases{"1.4.0"}}); err != nil {
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
