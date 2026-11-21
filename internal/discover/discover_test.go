package discover

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/kyleking/doneram/internal/engine"
	"github.com/kyleking/doneram/internal/locator"
)

const sha = "11d5960a326750d5838078e36cf38b85af677262"

func write(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestSitesFindsActionsAndTools(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, ".github/workflows/ci.yml", `jobs:
  a:
    steps:
      - uses: actions/checkout@`+sha+` # v4.4.0
      - uses: actions/checkout@`+sha+` # v4.4.0
      - uses: github/codeql-action/init@`+sha+` # v3.1.0
      - uses: github/codeql-action/analyze@`+sha+` # v3.1.0
      - uses: actions/setup-go@v5
      - uses: docker/login-action@`+sha+`
      - uses: ./local-action
`)
	write(t, dir, "mise.toml", `[env]
FOO = "1.2.3"

[tools]
go = "1.23"
"go:github.com/golangci/golangci-lint/v2/cmd/golangci-lint" = "2.12.2"
node = "lts"
python = ["3.12", "3.13"]
`)
	write(t, dir, ".tool-versions", "ruby 3.3.1\nnodejs latest\n")

	sites, err := Sites(dir)
	if err != nil {
		t.Fatal(err)
	}

	got := map[string]int{}
	for _, s := range sites {
		matches, err := locator.Find(s.Locator)
		if err != nil {
			t.Fatalf("%s: %v", s.Tool, err)
		}
		if err := locator.CheckExpect(s.Locator, matches); err != nil {
			t.Errorf("%s: %v", s.Tool, err)
		}
		got[s.Tool] = len(matches)
	}

	want := map[string]int{
		"actions/checkout":     2,
		"github/codeql-action": 2,
		"go":                   1,
		"go:github.com/golangci/golangci-lint/v2/cmd/golangci-lint": 1,
		"ruby": 1,
	}
	if len(got) != len(want) {
		t.Errorf("tools = %v, want %v", got, want)
	}
	for tool, n := range want {
		if got[tool] != n {
			t.Errorf("%s matches = %d, want %d", tool, got[tool], n)
		}
	}
}

func TestMergeDropsCoveredAndIgnored(t *testing.T) {
	dir := t.TempDir()
	ci := write(t, dir, ".github/workflows/ci.yml", "- uses: actions/checkout@"+sha+" # v4.4.0\n- uses: actions/setup-go@"+sha+" # v5.0.0\n- uses: jdx/mise-action@"+sha+" # v3.0.0\n")

	configured := []engine.Site{{
		Tool:    "checkout",
		Locator: locator.Locator{Glob: ci, Pattern: `actions/checkout@([0-9a-f]{40} # v[\d.]+)`, Resolver: "github-action"},
	}}
	found, err := Sites(dir)
	if err != nil {
		t.Fatal(err)
	}

	merged, err := Merge(configured, found, []string{"jdx/mise-action"})
	if err != nil {
		t.Fatal(err)
	}
	var tools []string
	for _, s := range merged {
		tools = append(tools, s.Tool)
	}
	if len(tools) != 2 || tools[0] != "checkout" || tools[1] != "actions/setup-go" {
		t.Errorf("merged tools = %v, want [checkout actions/setup-go]", tools)
	}
}
