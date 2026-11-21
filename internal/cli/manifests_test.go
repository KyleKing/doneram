package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/kyleking/doneram/internal/parser"
	"github.com/kyleking/doneram/internal/resolver"
)

// fixedResolver answers every package with the version in its map.
type fixedResolver map[string]string

func (f fixedResolver) Name() string { return "fixed" }

func (f fixedResolver) Resolve(_ context.Context, pkg string, _ *parser.VersionPattern) (string, error) {
	if v, ok := f[pkg]; ok {
		return v, nil
	}
	return "", fmt.Errorf("no package %s", pkg)
}

func (f fixedResolver) GetChangelog(context.Context, string, string, string) (string, error) {
	return "", nil
}

const (
	manifestPyproject = `[project]
name = "demo"
dependencies = [
    "requests>=2.31,<3",
    "httpx>=0.27",  # keep this comment
]
`
	manifestUVLock = `version = 1

[[package]]
name = "requests"
version = "2.31.0"

[[package]]
name = "httpx"
version = "0.27.0"
`
	manifestPackageJSON = `{
  "name": "web",
  "dependencies": {
    "lodash": "^4.17.0"
  }
}
`
	manifestPackageLock = `{"lockfileVersion": 3, "packages": {"node_modules/lodash": {"version": "4.17.21"}}}`
)

// manifestRepo lays out a uv project at the root and an npm project in
// web/, with stand-in uv and npm that record each call and, for `uv lock
// --upgrade`, move the lock forward the way the real one would.
func manifestRepo(t *testing.T, tools ...string) (dir, calls string) {
	t.Helper()
	dir = t.TempDir()
	files := map[string]string{
		".doneram.pkl":           "tools {}\nlockfiles = false\n",
		"pyproject.toml":         manifestPyproject,
		"uv.lock":                manifestUVLock,
		"web/package.json":       manifestPackageJSON,
		"web/package-lock.json":  manifestPackageLock,
		"upgraded.lock.fixture":  strings.NewReplacer("2.31.0", "2.32.3", "0.27.0", "0.28.1").Replace(manifestUVLock),
		"unrelated/pyproject.py": "not a project",
	}
	for name, body := range files {
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	bin := t.TempDir()
	calls = filepath.Join(bin, "calls")
	scripts := map[string]string{
		"uv": "#!/bin/sh\necho \"$PWD uv $*\" >> " + calls + "\n" +
			"if [ \"$1 $2\" = \"lock --upgrade\" ]; then /bin/cp " + filepath.Join(dir, "upgraded.lock.fixture") + " uv.lock; fi\n",
		"npm": "#!/bin/sh\necho \"$PWD npm $*\" >> " + calls + "\n",
	}
	for _, tool := range tools {
		if err := os.WriteFile(filepath.Join(bin, tool), []byte(scripts[tool]), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	// Only the stand-ins and pkl are on PATH, so a missing stand-in reads
	// as a package manager that is not installed.
	pkl, err := exec.LookPath("pkl")
	if err != nil {
		t.Skip("pkl not installed")
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+filepath.Dir(pkl))
	if _, err := exec.LookPath("uv"); err == nil && !contains(tools, "uv") {
		t.Skip("uv sits beside pkl, so it cannot be hidden")
	}
	return dir, calls
}

func contains(xs []string, x string) bool {
	for _, y := range xs {
		if y == x {
			return true
		}
	}
	return false
}

func manifestResolvers() map[string]resolver.Resolver {
	return map[string]resolver.Resolver{
		"pypi": fixedResolver{"requests": "3.1.0", "httpx": "0.28.1"},
		"npm":  fixedResolver{"lodash": "4.17.21"},
	}
}

func readSummary(t *testing.T, path string) pklSummary {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var s pklSummary
	if err := json.Unmarshal(data, &s); err != nil {
		t.Fatal(err)
	}
	return s
}

func readCalls(t *testing.T, path string) []string {
	t.Helper()
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	return strings.Split(strings.TrimSpace(string(data)), "\n")
}

func TestRunCheckPklManifests(t *testing.T) {
	t.Setenv("GITHUB_OUTPUT", "")
	dir, calls := manifestRepo(t, "uv", "npm")
	cfg := filepath.Join(dir, ".doneram.pkl")
	web := filepath.Join(dir, "web")
	out := filepath.Join(dir, "summary.json")

	// check reports the major outside the range and runs nothing.
	if err := runCheckPkl(context.Background(), pklRun{path: cfg, output: out, format: "json", workers: 1, resolvers: manifestResolvers()}); err != nil {
		t.Fatalf("check err = %v", err)
	}
	if got := readCalls(t, calls); got != nil {
		t.Errorf("check ran %v, want no package manager", got)
	}
	summary := readSummary(t, out)
	if len(summary.Results) != 1 {
		t.Fatalf("results = %+v, want the one major", summary.Results)
	}
	if r := summary.Results[0]; r.Tool != "pypi:requests" || r.Current != ">=2.31,<3" || r.Latest != ">=3.1.0,<4" || r.Risk != "review" {
		t.Errorf("row = %+v", r)
	}

	// A plan run upgrades both locks, raises the floors, relocks, and
	// leaves the major for its own group.
	if err := runCheckPkl(context.Background(), pklRun{path: cfg, apply: true, output: out, format: "json", workers: 1, resolvers: manifestResolvers()}); err != nil {
		t.Fatalf("plan err = %v", err)
	}
	wantCalls := []string{
		dir + " uv lock --upgrade",
		dir + " uv lock",
		web + " npm update --package-lock-only",
		web + " npm install --package-lock-only --ignore-scripts",
	}
	if got := readCalls(t, calls); !reflect.DeepEqual(got, wantCalls) {
		t.Errorf("calls = %q, want %q", got, wantCalls)
	}
	py, _ := os.ReadFile(filepath.Join(dir, "pyproject.toml"))
	wantPy := strings.NewReplacer(">=2.31,<3", ">=2.32.3,<3", ">=0.27", ">=0.28.1").Replace(manifestPyproject)
	if string(py) != wantPy {
		t.Errorf("pyproject.toml =\n%s\nwant\n%s", py, wantPy)
	}
	pj, _ := os.ReadFile(filepath.Join(web, "package.json"))
	if want := strings.Replace(manifestPackageJSON, "^4.17.0", "^4.17.21", 1); string(pj) != want {
		t.Errorf("package.json =\n%s\nwant\n%s", pj, want)
	}

	summary = readSummary(t, out)
	if len(summary.Manifests) != 2 || !summary.Manifests[0].Changed || !summary.Manifests[1].Changed {
		t.Fatalf("manifests = %+v, want two changed projects", summary.Manifests)
	}
	if got := len(summary.Manifests[0].Floors) + len(summary.Manifests[1].Floors); got != 3 {
		t.Errorf("floors raised = %d, want 3", got)
	}
	var ids []string
	for _, g := range summary.Groups {
		ids = append(ids, g.ID)
	}
	if want := []string{"updates", "review-pypi-requests"}; !reflect.DeepEqual(ids, want) {
		t.Fatalf("groups = %v, want %v", ids, want)
	}
	updates := summary.Groups[0]
	if !strings.Contains(updates.Body, "### Raised declared floors") || !strings.Contains(updates.Body, "| `pyproject.toml` | `requests` | `>=2.31,<3` | `>=2.32.3,<3` |") {
		t.Errorf("updates body missing the floors table:\n%s", updates.Body)
	}
	if strings.Contains(updates.Body, "pypi:requests") {
		t.Errorf("updates body carries the review bump:\n%s", updates.Body)
	}
	if review := summary.Groups[1]; !strings.Contains(review.Body, ">=3.1.0,<4") || strings.Contains(review.Body, "Raised declared floors") {
		t.Errorf("review body:\n%s", review.Body)
	}

	// The review group, on a fresh checkout, widens the range and relocks,
	// and touches nothing else.
	for name, body := range map[string]string{"pyproject.toml": manifestPyproject, "uv.lock": manifestUVLock, "web/package.json": manifestPackageJSON} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	_ = os.Remove(calls)
	group, err := loadGroup(out, "review-pypi-requests")
	if err != nil {
		t.Fatal(err)
	}
	groupOut := filepath.Join(dir, "group.json")
	if err := runCheckPkl(context.Background(), pklRun{path: cfg, apply: true, output: groupOut, format: "json", workers: 1, resolvers: manifestResolvers(), group: group}); err != nil {
		t.Fatalf("group err = %v", err)
	}
	if got, want := readCalls(t, calls), []string{dir + " uv lock"}; !reflect.DeepEqual(got, want) {
		t.Errorf("group calls = %q, want %q", got, want)
	}
	py, _ = os.ReadFile(filepath.Join(dir, "pyproject.toml"))
	if want := strings.Replace(manifestPyproject, ">=2.31,<3", ">=3.1.0,<4", 1); string(py) != want {
		t.Errorf("pyproject.toml after the review group =\n%s\nwant\n%s", py, want)
	}
	pj, _ = os.ReadFile(filepath.Join(web, "package.json"))
	if string(pj) != manifestPackageJSON {
		t.Errorf("review group touched package.json:\n%s", pj)
	}
	if s := readSummary(t, groupOut); !s.HasUpgrades || len(s.Manifests) != 0 {
		t.Errorf("group summary = %+v, want an upgrade and no floor sync", s)
	}
}

func TestRunCheckPklManifestsSkipsMissingTool(t *testing.T) {
	t.Setenv("GITHUB_OUTPUT", "")
	dir, calls := manifestRepo(t, "npm")
	cfg := filepath.Join(dir, ".doneram.pkl")
	out := filepath.Join(dir, "summary.json")
	if err := runCheckPkl(context.Background(), pklRun{path: cfg, apply: true, output: out, format: "json", workers: 1, resolvers: manifestResolvers()}); err != nil {
		t.Fatalf("err = %v", err)
	}
	summary := readSummary(t, out)
	if len(summary.Manifests) != 2 {
		t.Fatalf("manifests = %+v", summary.Manifests)
	}
	uv := summary.Manifests[0]
	if uv.Tool != "uv" || uv.Command != "uv lock --upgrade" || uv.Changed {
		t.Errorf("uv project = %+v, want it skipped with the command to run", uv)
	}
	if got := readCalls(t, calls); len(got) != 2 || !strings.Contains(got[0], "npm update") {
		t.Errorf("calls = %q, want only npm's", got)
	}
	py, _ := os.ReadFile(filepath.Join(dir, "pyproject.toml"))
	if string(py) != manifestPyproject {
		t.Errorf("pyproject.toml changed without uv:\n%s", py)
	}
}

func TestRunCheckPklManifestsDisabled(t *testing.T) {
	t.Setenv("GITHUB_OUTPUT", "")
	dir, calls := manifestRepo(t, "uv", "npm")
	cfg := filepath.Join(dir, ".doneram.pkl")
	if err := os.WriteFile(cfg, []byte("tools {}\nlockfiles = false\nmanifests = false\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "summary.json")
	if err := runCheckPkl(context.Background(), pklRun{path: cfg, apply: true, output: out, format: "json", workers: 1, resolvers: manifestResolvers()}); err != nil {
		t.Fatalf("err = %v", err)
	}
	if got := readCalls(t, calls); got != nil {
		t.Errorf("calls = %q, want none", got)
	}
	if s := readSummary(t, out); s.HasUpgrades || len(s.Results) != 0 {
		t.Errorf("summary = %+v, want nothing", s)
	}
}
