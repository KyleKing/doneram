package copier

import (
	"os"
	"path/filepath"
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
