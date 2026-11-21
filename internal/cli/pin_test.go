package cli

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type stubPin struct{}

func (stubPin) PinAction(context.Context, string, string) (string, error) {
	return strings.Repeat("b", 40) + " # v4.2.2", nil
}

func (stubPin) PinTool(context.Context, string, string) (string, error) {
	return "1.23.4", nil
}

func TestRunCheckPklHardens(t *testing.T) {
	t.Setenv("GITHUB_OUTPUT", "")
	dir := t.TempDir()
	cfg := filepath.Join(dir, ".doneram.pkl")
	if err := os.WriteFile(cfg, []byte("tools {}\nlockfiles = false\nignore { \"actions/checkout\" }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	wf := filepath.Join(dir, ".github", "workflows", "ci.yml")
	if err := os.MkdirAll(filepath.Dir(wf), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(wf, []byte("steps:\n  - uses: actions/checkout@v4\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	err := runCheckPkl(context.Background(), pklRun{path: cfg, format: "json", workers: 1, pin: stubPin{}, failOnUnpinned: true})
	var exit *ExitError
	if !errors.As(err, &exit) || exit.Code != ExitUnpinned {
		t.Fatalf("check err = %v, want exit %d", err, ExitUnpinned)
	}

	out := filepath.Join(dir, "summary.json")
	// The ignore keeps the newly pinned action out of discovery, so the run
	// never reaches the GitHub API.
	if err := runCheckPkl(context.Background(), pklRun{path: cfg, apply: true, output: out, format: "json", workers: 1, pin: stubPin{}, failOnUnpinned: true}); err != nil {
		t.Fatalf("apply err = %v", err)
	}
	data, _ := os.ReadFile(wf)
	if want := "- uses: actions/checkout@" + strings.Repeat("b", 40) + " # v4.2.2"; !strings.Contains(string(data), want) {
		t.Fatalf("workflow = %s, want %s", data, want)
	}
	var summary pklSummary
	raw, _ := os.ReadFile(out)
	if err := json.Unmarshal(raw, &summary); err != nil {
		t.Fatal(err)
	}
	if len(summary.Hardened) != 1 || !summary.Hardened[0].Updated || !strings.Contains(summary.Body, "Pinned floating references") {
		t.Errorf("summary = %+v", summary)
	}
}
