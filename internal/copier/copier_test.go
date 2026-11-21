package copier

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestScan(t *testing.T) {
	dir := t.TempDir()
	write := func(rel, content string) {
		path := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(".copier-answers.yml", "_commit: v1.2.0\n_src_path: gh:kyleking/calcipy_template\nproject: x\n")
	write("sub/.copier-answers.docs.yaml", "_commit: 2.0.0\n_src_path: https://github.com/o/docs-template.git\n")
	write("other/.copier-answers.yml", "_commit: v1.0.0\n_src_path: https://gitlab.com/o/t\n")
	write("deep/nested/.copier-answers.yml", "_commit: v1\n_src_path: gh:o/ignored\n")

	got, err := Scan(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("Scan() = %+v", got)
	}
	if got[0].Repo != "kyleking/calcipy_template" || got[0].Commit != "v1.2.0" {
		t.Errorf("root = %+v", got[0])
	}
	if got[1].Repo != "" {
		t.Errorf("gitlab template = %+v, want no GitHub repo", got[1])
	}
	if got[2].Repo != "o/docs-template" || got[2].Commit != "2.0.0" {
		t.Errorf("sub = %+v", got[2])
	}
	if !Released("v1.2.3") || Released("v1.2.3-4-gabcdef1") {
		t.Error("Released misjudges a tag or a describe string")
	}
}

func TestOwned(t *testing.T) {
	s, err := ParseSettings([]byte("_subdirectory: go_template\n_skip_if_exists:\n  - README.md\n  - go.mod\n  - cmd/\n"))
	if err != nil {
		t.Fatal(err)
	}
	files := []string{
		"copier.yml",
		"README.md",
		"go_template/README.md.jinja",
		"go_template/go.mod.jinja",
		"go_template/cmd/main.go.jinja",
		"go_template/.github/workflows/ci.yml.jinja",
		"go_template/.github/workflows/freshness.yml",
		"go_template/{% if publish_python %}bindings{% endif %}/pyproject.toml.jinja",
		"go_template/{{ project_name }}.go.jinja",
		"go_template/{{ _copier_conf.answers_file }}.jinja",
	}
	got := strings.Join(Owned(files, s, ".copier-answers.yml"), ",")
	want := ".github/workflows/ci.yml,.github/workflows/freshness.yml,bindings/pyproject.toml"
	if got != want {
		t.Errorf("Owned = %s, want %s", got, want)
	}
}

func TestTrusted(t *testing.T) {
	tmpl := Template{Source: "gh:KyleKing/my_go_template", Repo: "KyleKing/my_go_template"}
	cases := []struct {
		owner  string
		listed []string
		want   bool
	}{
		{"kyleking", nil, true},
		{"someone", nil, false},
		{"", nil, false},
		{"someone", []string{"gh:KyleKing/my_go_template"}, true},
		{"someone", []string{"kyleking/my_go_template"}, true},
		{"someone", []string{"gh:other/template"}, false},
	}
	for _, c := range cases {
		if got := Trusted(tmpl, c.owner, c.listed); got != c.want {
			t.Errorf("Trusted(owner=%q, listed=%v) = %v, want %v", c.owner, c.listed, got, c.want)
		}
	}
	if m := ownerOf.FindStringSubmatch("https://x-access-token:abc@github.com/acme/app.git"); m == nil || m[1] != "acme" {
		t.Errorf("ownerOf = %v", m)
	}
}
