package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/kyleking/doneram/internal/fleet"
)

// fleetRemote makes a bare repo with files on main, standing in for the
// GitHub side of a fleet run's clone and push.
func fleetRemote(t *testing.T, files map[string]string) string {
	t.Helper()
	work := t.TempDir()
	git(t, work, "init", "-q", "-b", "main")
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(work, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	git(t, work, "add", ".")
	git(t, work, "commit", "-qm", "init")
	remote := filepath.Join(t.TempDir(), "remote.git")
	git(t, work, "clone", "-q", "--bare", work, remote)
	return remote
}

func showRef(t *testing.T, remote, spec string) (string, error) {
	t.Helper()
	out, err := exec.Command("git", "-C", remote, "show", spec).CombinedOutput()
	return string(out), err
}

// prServer records the pull requests a fleet run opens.
type prServer struct {
	mu      sync.Mutex
	created []map[string]any
}

func (p *prServer) start(t *testing.T) *httptest.Server {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p.mu.Lock()
		defer p.mu.Unlock()
		switch {
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/pulls"):
			_, _ = w.Write([]byte(`[]`))
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/pulls"):
			var req map[string]any
			_ = json.NewDecoder(r.Body).Decode(&req)
			p.created = append(p.created, req)
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"number":1,"html_url":"https://github.test` + strings.TrimSuffix(r.URL.Path, "s") + `/1"}`))
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func fleetRunner(srv *httptest.Server, remotes map[string]string) *fleet.Runner {
	return &fleet.Runner{
		GitHub:   fleet.NewGitHub(srv.URL, "tok"),
		Git:      &fleet.Git{Token: "tok", Server: "https://github.test"},
		Planner:  fleetPlanner{workers: 2},
		CloneURL: func(full string) string { return "file://" + remotes[full] },
	}
}

// TestFleetPlansAndShipsGroups runs the real plan in process on two
// clones at once: one with a config whose validate fails on one bump, so
// it plans two groups, and one with nothing to update.
func TestFleetPlansAndShipsGroups(t *testing.T) {
	t.Setenv("GITHUB_OUTPUT", "")
	app := fleetRemote(t, map[string]string{".doneram.pkl": bisectPkl, "deps.txt": "good>=1.0.0\nbad>=1.0.0\n"})
	docs := fleetRemote(t, map[string]string{"README.md": "hello\n"})
	prs := &prServer{}
	runner := fleetRunner(prs.start(t), map[string]string{"acme/app": app, "acme/docs": docs})
	repos := []fleet.Repo{{FullName: "acme/app", DefaultBranch: "main"}, {FullName: "acme/docs", DefaultBranch: "main"}}

	report, err := runner.Run(context.Background(), repos, fleet.Options{Workers: 2})
	if err != nil {
		t.Fatal(err)
	}
	rep := report.Repos[0]
	if rep.Status != fleet.StatusOK || len(rep.Errors) != 0 {
		t.Fatalf("app report = %+v", rep)
	}
	var ids []string
	for _, g := range rep.Groups {
		ids = append(ids, g.ID+"="+g.Action)
	}
	if strings.Join(ids, " ") != "updates=opened failing-bad=opened" {
		t.Fatalf("groups = %v", ids)
	}
	if rep.Counts.Updates != 2 {
		t.Errorf("counts = %+v", rep.Counts)
	}
	if got, _ := showRef(t, app, "doneram/updates:deps.txt"); got != "good>=1.1.0\nbad>=1.0.0\n" {
		t.Errorf("doneram/updates deps.txt = %q", got)
	}
	if got, _ := showRef(t, app, "doneram/failing-bad:deps.txt"); got != "good>=1.0.0\nbad>=2.0.0\n" {
		t.Errorf("doneram/failing-bad deps.txt = %q", got)
	}
	if got, _ := showRef(t, app, "main:deps.txt"); got != "good>=1.0.0\nbad>=1.0.0\n" {
		t.Errorf("main deps.txt = %q", got)
	}
	if len(prs.created) != 2 || prs.created[1]["head"] != "doneram/failing-bad" || prs.created[1]["draft"] != true ||
		!strings.Contains(prs.created[1]["title"].(string), "validation fails") {
		t.Errorf("created = %v", prs.created)
	}

	if d := report.Repos[1]; d.Status != fleet.StatusOK || len(d.Groups) != 0 || len(d.Errors) != 0 {
		t.Errorf("docs report = %+v", d)
	}
}

func TestFleetDryRunPushesNothing(t *testing.T) {
	t.Setenv("GITHUB_OUTPUT", "")
	app := fleetRemote(t, map[string]string{".doneram.pkl": driftPkl, "pyproject.toml": "uvicorn>=0.52.3\n"})
	prs := &prServer{}
	runner := fleetRunner(prs.start(t), map[string]string{"acme/app": app})

	report, err := runner.Run(context.Background(), []fleet.Repo{{FullName: "acme/app", DefaultBranch: "main"}}, fleet.Options{DryRun: true})
	if err != nil {
		t.Fatal(err)
	}
	rep := report.Repos[0]
	if len(rep.Groups) != 1 || rep.Groups[0].Action != fleet.ActionPlanned || rep.Counts.Updates != 1 {
		t.Errorf("report = %+v", rep)
	}
	if out, _ := exec.Command("git", "-C", app, "for-each-ref", "--format=%(refname)").Output(); strings.TrimSpace(string(out)) != "refs/heads/main" {
		t.Errorf("dry run left refs %q", out)
	}
	if len(prs.created) != 0 {
		t.Errorf("dry run opened %v", prs.created)
	}
}

func TestWriteFleetReport(t *testing.T) {
	dir := t.TempDir()
	stepSummary := filepath.Join(dir, "summary.md")
	t.Setenv("GITHUB_STEP_SUMMARY", stepSummary)
	report := &fleet.Report{Repos: []fleet.RepoReport{
		{Repo: "acme/app", Status: fleet.StatusOK, Counts: fleet.Counts{Updates: 3, Security: 1, EOL: 1, EOLPast: 1},
			Groups: []fleet.GroupReport{{ID: "updates", Action: fleet.ActionOpened, Number: 4, URL: "https://github.test/acme/app/pull/4"}}},
		{Repo: "acme/gone", Status: fleet.StatusFailed, Errors: []string{"clone: not found"}},
	}}
	var stdout bytes.Buffer
	mdPath, jsonPath := filepath.Join(dir, "report.md"), filepath.Join(dir, "report.json")
	err := writeFleetReport(report, &stdout, mdPath, jsonPath)
	var exit *ExitError
	if !errors.As(err, &exit) || exit.Code != ExitUnresolved {
		t.Errorf("err = %v, want exit %d for a failed repo", err, ExitUnresolved)
	}
	md := stdout.String()
	for _, want := range []string{"[#4](https://github.test/acme/app/pull/4) opened", "| `acme/app` | 3 | 1 |", "**failed**", "clone: not found"} {
		if !strings.Contains(md, want) {
			t.Errorf("markdown lacks %q:\n%s", want, md)
		}
	}
	if data, _ := os.ReadFile(mdPath); string(data) != md {
		t.Error("report file differs from stdout")
	}
	if data, _ := os.ReadFile(stepSummary); !strings.Contains(string(data), md) {
		t.Error("step summary missing the report")
	}
	var back fleet.Report
	data, _ := os.ReadFile(jsonPath)
	if err := json.Unmarshal(data, &back); err != nil || back.Repos[0].Counts.Updates != 3 {
		t.Errorf("json = %s (%v)", data, err)
	}

	report.Repos = report.Repos[:1]
	if err := writeFleetReport(report, &stdout, "", ""); err != nil {
		t.Errorf("a run with only group-level problems should exit 0: %v", err)
	}
}

func TestFleetReposSelection(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/orgs/acme/repos":
			_, _ = w.Write([]byte(`[{"full_name":"acme/zeta","default_branch":"main"},{"full_name":"acme/Alpha","default_branch":"main"},
				{"full_name":"acme/fork","fork":true},{"full_name":"acme/old","archived":true},{"full_name":"acme/skip"}]`))
		case "/repos/other/one":
			_, _ = w.Write([]byte(`{"full_name":"other/one","default_branch":"dev","archived":true}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	list := filepath.Join(t.TempDir(), "repos.txt")
	if err := os.WriteFile(list, []byte("# fleet\nother/two  # trailing comment\n\nacme/zeta\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	repos, err := fleetRepos(context.Background(), fleet.NewGitHub(srv.URL, ""), []string{"other/one"}, list, "acme", []string{"skip", "ACME/nothing"})
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, r := range repos {
		got = append(got, r.FullName+"@"+r.DefaultBranch)
	}
	// Named repos come first and are kept even when archived; the org
	// listing is sorted, drops forks and archives, and dedupes.
	if strings.Join(got, " ") != "other/one@dev other/two@ acme/zeta@ acme/Alpha@main" {
		t.Errorf("repos = %v", got)
	}

	if _, err := fleetRepos(context.Background(), fleet.NewGitHub(srv.URL, ""), []string{"not a repo"}, "", "", nil); err == nil {
		t.Error("accepted a malformed repo name")
	}
}

func TestRunFleetDropsWriteTokenFromEnvironment(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "write-token")
	t.Setenv("GH_TOKEN", "write-token")
	t.Setenv("DONERAM_READ_TOKEN", "read-token")
	t.Setenv("GITHUB_API_URL", "http://127.0.0.1:1")
	// The run fails to list repos (nothing listens there), but the token
	// swap happens before any repo is touched.
	_ = NewApp("test", "", "").Run(context.Background(), []string{"doneram", "fleet", "--repo", "o/r", "--workdir", t.TempDir()})
	if got := os.Getenv("GITHUB_TOKEN"); got != "read-token" {
		t.Errorf("GITHUB_TOKEN = %q, want the read token", got)
	}
	if os.Getenv("GH_TOKEN") != "" || os.Getenv("DONERAM_READ_TOKEN") != "" {
		t.Error("write or read token left in its original variable")
	}
}
