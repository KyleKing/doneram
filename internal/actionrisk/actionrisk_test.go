package actionrisk

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const v1 = `
inputs:
  token:
    default: ${{ github.token }}
  fetch-depth:
    default: 1
  legacy:
    description: old
outputs:
  ref:
    description: x
runs:
  using: node20
`

const v2 = `
inputs:
  token:
    default: ${{ github.token }}
  fetch-depth:
    default: 0
  path:
    required: true
outputs:
  commit:
    description: x
runs:
  using: node24
`

func server(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/contents/action.yml") && r.URL.Query().Get("ref") == "v1.2.0":
			_, _ = w.Write([]byte(v1))
		case strings.HasSuffix(r.URL.Path, "/contents/action.yml") && r.URL.Query().Get("ref") == "v2.0.1":
			_, _ = w.Write([]byte(v2))
		case strings.HasSuffix(r.URL.Path, "/releases/tags/v2.0.0"):
			_, _ = w.Write([]byte(`{"body":"## What's changed\n- BREAKING: drop legacy input\n- fix thing"}`))
		default:
			http.NotFound(w, r)
		}
	}))
}

func TestAssess(t *testing.T) {
	srv := server(t)
	defer srv.Close()
	c := NewWithBaseURL(srv.Client(), srv.URL)

	cases := []struct {
		name      string
		usage     Usage
		wantLevel Level
		wantNotes []string
	}{
		{
			name:      "untouched interface is safe",
			usage:     Usage{With: map[string]bool{"token": true, "fetch-depth": true, "path": true}},
			wantLevel: Safe,
			wantNotes: []string{"runtime node20 → node24", "release notes: v2: BREAKING: drop legacy input"},
		},
		{
			name:      "removed input, new required input, removed output",
			usage:     Usage{With: map[string]bool{"legacy": true, "fetch-depth": true}, Outputs: map[string]bool{"ref": true}},
			wantLevel: Breaking,
			wantNotes: []string{"input `legacy` was removed", "input `path` is newly required", "output `ref` was removed"},
		},
		{
			name:      "changed default the step relies on",
			usage:     Usage{With: map[string]bool{"path": true}},
			wantLevel: Review,
			wantNotes: []string{"default of `fetch-depth` changed from \"1\" to \"0\""},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a, err := c.Assess(context.Background(), "o/r", "v1.2.0", "v2.0.1", []Usage{tc.usage})
			if err != nil {
				t.Fatal(err)
			}
			if a.Level != tc.wantLevel {
				t.Errorf("Level = %s, want %s (notes %q)", a.Level, tc.wantLevel, a.Notes)
			}
			joined := strings.Join(a.Notes, "\n")
			for _, want := range tc.wantNotes {
				if !strings.Contains(joined, want) {
					t.Errorf("notes %q missing %q", a.Notes, want)
				}
			}
		})
	}
}

func TestUsageAt(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ci.yml")
	content := `jobs:
  a:
    steps:
      - id: co
        uses: github/codeql-action/init@abc # v3.0.0
        with:
          languages: go
          queries: x
      - run: echo ${{ steps.co.outputs.db }} ${{ steps.other.outputs.no }}
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	u, err := UsageAt(path, 5)
	if err != nil {
		t.Fatal(err)
	}
	if u.Path != "init" || !u.With["languages"] || !u.With["queries"] || len(u.With) != 2 {
		t.Errorf("usage = %+v", u)
	}
	if !u.Outputs["db"] || len(u.Outputs) != 1 {
		t.Errorf("outputs = %v", u.Outputs)
	}
	if _, err := UsageAt(path, 2); err == nil {
		t.Error("expected an error for a line with no uses")
	}
}

func TestIsMajor(t *testing.T) {
	if !IsMajor("v4.4.0", "v7.0.1") || IsMajor("v4.1.0", "v4.4.0") {
		t.Error("IsMajor misclassified")
	}
}
