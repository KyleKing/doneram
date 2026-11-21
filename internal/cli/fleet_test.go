package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kyleking/doneram/internal/fleet"
)

// fleetRepo makes a git repo at parent/name holding files on main, with a
// local identity so a fleet --apply run can commit.
func fleetRepo(t *testing.T, parent, name string, files map[string]string) string {
	t.Helper()
	dir := filepath.Join(parent, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	git(t, dir, "init", "-q", "-b", "main")
	git(t, dir, "config", "user.email", "t@example.com")
	git(t, dir, "config", "user.name", "t")
	git(t, dir, "config", "commit.gpgsign", "false")
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	git(t, dir, "add", ".")
	git(t, dir, "commit", "-qm", "init")
	return dir
}

func gitOut(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v: %s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

// TestFleetChecksAndApplies runs the real check in process over a parent
// directory, then applies it.
func TestFleetChecksAndApplies(t *testing.T) {
	t.Setenv("GITHUB_OUTPUT", "")
	root := t.TempDir()
	app := fleetRepo(t, root, "app", map[string]string{".doneram.pkl": driftPkl, "pyproject.toml": "uvicorn>=0.52.3\n"})
	fleetRepo(t, root, "skipped", map[string]string{"README.md": "hello\n"})
	jsonPath := filepath.Join(t.TempDir(), "fleet.json")
	run := func(args ...string) fleet.Report {
		t.Helper()
		args = append([]string{"doneram", "fleet", "--dir", root, "--exclude", "^skip", "--json", jsonPath}, args...)
		if err := NewApp("test", "", "").Run(context.Background(), args); err != nil {
			t.Fatal(err)
		}
		var report fleet.Report
		data, _ := os.ReadFile(jsonPath)
		if err := json.Unmarshal(data, &report); err != nil {
			t.Fatal(err)
		}
		return report
	}

	report := run()
	if len(report.Repos) != 1 {
		t.Fatalf("repos = %+v", report.Repos)
	}
	rep := report.Repos[0]
	if rep.Repo != "app" || rep.Counts.Updates != 1 || rep.Commit != "" || len(rep.Changes) != 1 || rep.Changes[0] != "uvicorn 0.52.3 -> 0.52.4" {
		t.Errorf("check report = %+v", rep)
	}
	if data, _ := os.ReadFile(filepath.Join(app, "pyproject.toml")); string(data) != "uvicorn>=0.52.3\n" {
		t.Error("a check run patched the tree")
	}

	rep = run("--apply").Repos[0]
	if rep.Status != fleet.StatusOK || rep.Branch != fleet.Branch || rep.Commit == "" {
		t.Fatalf("apply report = %+v", rep)
	}
	if got := gitOut(t, app, "symbolic-ref", "--short", "HEAD"); got != "main" {
		t.Errorf("checked out %q, want main", got)
	}
	if got := gitOut(t, app, "status", "--porcelain"); got != "" {
		t.Errorf("tree left dirty: %q", got)
	}
	if got := gitOut(t, app, "show", fleet.Branch+":pyproject.toml"); got != "uvicorn>=0.52.4" {
		t.Errorf("%s pyproject.toml = %q", fleet.Branch, got)
	}
	if got := gitOut(t, app, "show", "main:pyproject.toml"); got != "uvicorn>=0.52.3" {
		t.Errorf("main pyproject.toml = %q", got)
	}
}

func TestFleetNoMatch(t *testing.T) {
	err := NewApp("test", "", "").Run(context.Background(), []string{"doneram", "fleet", "--dir", t.TempDir()})
	var exit *ExitError
	if !errors.As(err, &exit) || exit.Code != ExitConfig {
		t.Errorf("err = %v, want exit %d", err, ExitConfig)
	}
}

func TestWriteFleetReport(t *testing.T) {
	dir := t.TempDir()
	report := &fleet.Report{Apply: true, Repos: []fleet.RepoReport{
		{Repo: "app", Status: fleet.StatusOK, Counts: fleet.Counts{Updates: 3, Vulnerable: 1, EOL: 1, EOLPast: 1},
			Changes: []string{"uv 1 -> 2"}, Branch: fleet.Branch, Commit: "0123456789abcdef"},
		{Repo: "gone", Status: fleet.StatusFailed, Errors: []string{"git switch: failed"}},
	}}
	var stdout bytes.Buffer
	mdPath, jsonPath := filepath.Join(dir, "report.md"), filepath.Join(dir, "report.json")
	err := writeFleetReport(report, &stdout, mdPath, jsonPath)
	var exit *ExitError
	if !errors.As(err, &exit) || exit.Code != ExitUnresolved {
		t.Errorf("err = %v, want exit %d for a failed repo", err, ExitUnresolved)
	}
	md := stdout.String()
	for _, want := range []string{"committed 0123456789ab on doneram/updates", "| `app` | 3 | 1 |", "- uv 1 -> 2", "failed", "git switch: failed"} {
		if !strings.Contains(md, want) {
			t.Errorf("markdown lacks %q:\n%s", want, md)
		}
	}
	if data, _ := os.ReadFile(mdPath); string(data) != md {
		t.Error("report file differs from stdout")
	}
	var back fleet.Report
	data, _ := os.ReadFile(jsonPath)
	if err := json.Unmarshal(data, &back); err != nil || back.Repos[0].Counts.Updates != 3 {
		t.Errorf("json = %s (%v)", data, err)
	}

	report.Repos = report.Repos[:1]
	if err := writeFleetReport(report, &stdout, "", ""); err != nil {
		t.Errorf("a run with no failed repo should exit 0: %v", err)
	}
}
