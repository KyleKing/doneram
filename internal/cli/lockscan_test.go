package cli

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kyleking/doneram/internal/osv"
)

// fakeOSV answers every batch query for urllib3 with one advisory fixed in
// 1.26.5, and nothing for anything else.
func fakeOSV(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/querybatch":
			body, _ := io.ReadAll(r.Body)
			var req struct {
				Queries []struct {
					Package struct{ Name string } `json:"package"`
				} `json:"queries"`
			}
			_ = json.Unmarshal(body, &req)
			type vuln struct {
				ID string `json:"id"`
			}
			results := make([]map[string][]vuln, len(req.Queries))
			for i, q := range req.Queries {
				results[i] = map[string][]vuln{}
				if q.Package.Name == "urllib3" {
					results[i]["vulns"] = []vuln{{ID: "GHSA-test"}}
				}
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"results": results})
		case "/v1/vulns/GHSA-test":
			_, _ = io.WriteString(w, `{"id":"GHSA-test","affected":[
				{"package":{"name":"urllib3","ecosystem":"PyPI"},"ranges":[{"type":"ECOSYSTEM","events":[{"introduced":"0"},{"fixed":"1.26.5"}]}]},
				{"package":{"name":"other","ecosystem":"PyPI"},"ranges":[{"type":"ECOSYSTEM","events":[{"introduced":"0"},{"fixed":"1.25.0"}]}]}
			]}`)
		default:
			http.NotFound(w, r)
		}
	}))
}

const uvLock = `version = 1

[[package]]
name = "requests"
version = "2.31.0"

[[package]]
name = "urllib3"
version = "1.24.1"
`

func TestRunCheckPklLockfiles(t *testing.T) {
	t.Setenv("GITHUB_OUTPUT", "")
	server := fakeOSV(t)
	defer server.Close()

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "uv.lock"), []byte(uvLock), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := filepath.Join(dir, ".doneram.pkl")
	if err := os.WriteFile(cfg, []byte("tools {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// A stand-in uv records how it was called.
	bin := t.TempDir()
	calls := filepath.Join(bin, "calls")
	script := "#!/bin/sh\necho \"$PWD $@\" >> " + calls + "\n"
	if err := os.WriteFile(filepath.Join(bin, "uv"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))

	client := osv.NewWithBaseURL(&http.Client{}, server.URL)

	// Report only: a vulnerable lock fails the CVE gate.
	err := runCheckPkl(context.Background(), pklRun{path: cfg, format: "json", workers: 1, osv: client, failOnVuln: true})
	var exit *ExitError
	if !errors.As(err, &exit) || exit.Code != ExitVulnerable {
		t.Fatalf("err = %v, want exit %d", err, ExitVulnerable)
	}

	out := filepath.Join(dir, "summary.json")
	err = runCheckPkl(context.Background(), pklRun{path: cfg, apply: true, output: out, format: "json", workers: 1, osv: client, failOnVuln: true})
	if err != nil {
		t.Fatalf("apply err = %v", err)
	}
	got, err := os.ReadFile(calls)
	if err != nil {
		t.Fatal(err)
	}
	if want := dir + " lock --upgrade-package urllib3==1.26.5"; strings.TrimSpace(string(got)) != want {
		t.Errorf("uv called as %q, want %q", got, want)
	}

	var summary pklSummary
	data, _ := os.ReadFile(out)
	if err := json.Unmarshal(data, &summary); err != nil {
		t.Fatal(err)
	}
	if !summary.HasUpgrades || len(summary.Lockfiles) != 1 || !summary.Lockfiles[0].Updated {
		t.Fatalf("summary = %+v, want one applied lockfile fix", summary)
	}
	if summary.Lockfiles[0].Fixed != "1.26.5" {
		t.Errorf("fixed = %s, want 1.26.5 (the other package's 1.25.0 must not leak in)", summary.Lockfiles[0].Fixed)
	}
	if !strings.Contains(summary.Title, "clearing advisories") || !strings.Contains(summary.Body, "Lockfile security fixes") {
		t.Errorf("title %q / body %q missing the lockfile fix", summary.Title, summary.Body)
	}
}
