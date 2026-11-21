package changelog

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestChangelogSection(t *testing.T) {
	md := "# Changelog\n\n## [3.1.0] - 2024\n- later\n\n## [3.0.0] - 2024-01-01\n### Breaking\n- Removed `old_api` entirely\n\n## 2.9.0\n- earlier\n"
	got := changelogSection(md, 3)
	if !strings.Contains(got, "old_api") || strings.Contains(got, "earlier") || strings.Contains(got, "later") {
		t.Errorf("markdown section = %q", got)
	}
	rst := "Changes\n=======\n\n3.0.0 (2024-01-01)\n------------------\n\n- Dropped support for Python 3.8\n\n2.0.0\n-----\n\n- old\n"
	got = changelogSection(rst, 3)
	if !strings.Contains(got, "Python 3.8") || strings.Contains(got, "old") {
		t.Errorf("rst section = %q", got)
	}
	if changelogSection(md, 4) != "" {
		t.Error("found a section for a major with none")
	}
}

func TestBreakingLines(t *testing.T) {
	notes := "## Breaking changes\n- `parse_obj` was removed; use `model_validate`\n- Faster parsing\n* No longer accepts `None` for `timeout`\n- Breaking changes\n"
	got := breakingLines(notes)
	if len(got) != 2 || !strings.Contains(got[0], "parse_obj") || !strings.Contains(got[1], "No longer") {
		t.Errorf("breakingLines() = %q", got)
	}
}

func TestAssess(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/pypi/pydantic/json":
			_, _ = w.Write([]byte(`{"info":{"project_urls":{"Funding":"https://github.com/sponsors/x","Source":"https://github.com/pydantic/pydantic"}}}`))
		case "/repos/pydantic/pydantic/releases/tags/v2.0.0":
			_, _ = w.Write([]byte("{\"body\":\"- `parse_obj` was removed, use `model_validate`\\n- `Config.orm_mode` renamed to `from_attributes`\"}"))
		case "/pypi/quiet/json":
			_, _ = w.Write([]byte(`{"info":{"project_urls":{"Source":"https://github.com/o/quiet"}}}`))
		case "/repos/o/quiet/releases/tags/2.0.0":
			_, _ = w.Write([]byte(`{"body":"- Faster\n- New API"}`))
		case "/pypi/nosrc/json":
			_, _ = w.Write([]byte(`{"info":{"project_urls":{"Homepage":"https://example.com"}}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	urls := map[string]string{"pypi": server.URL, "github": server.URL, "npm": server.URL, "cargo": server.URL}

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "app.py"), []byte("m = Model.parse_obj(data)\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	a, err := NewWithBaseURLs(&http.Client{}, urls).Assess(ctx, "pypi", "pydantic", "1.10.0", "2.5.0", dir)
	if err != nil || a.Level != Breaking || !strings.Contains(strings.Join(a.Notes, "\n"), "this repo uses `parse_obj`") {
		t.Errorf("used removal = %+v, %v", a, err)
	}

	a, err = NewWithBaseURLs(&http.Client{}, urls).Assess(ctx, "pypi", "pydantic", "1.10.0", "2.5.0", t.TempDir())
	if err != nil || a.Level != Review {
		t.Errorf("unused removal = %+v, %v; want review", a, err)
	}

	a, err = NewWithBaseURLs(&http.Client{}, urls).Assess(ctx, "pypi", "quiet", "1.0.0", "2.1.0", dir)
	if err != nil || a.Level != Safe {
		t.Errorf("nothing breaking = %+v, %v; want safe", a, err)
	}

	a, err = NewWithBaseURLs(&http.Client{}, urls).Assess(ctx, "pypi", "nosrc", "1.0.0", "2.0.0", dir)
	if err != nil || a.Level != Review {
		t.Errorf("no source = %+v, %v; want review", a, err)
	}
}
