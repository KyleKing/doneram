package lifecycle

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func write(t *testing.T, dir, name, content string) {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestScan(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "mise.toml", "[tools]\npython = \"3.9.18\"\nnode = \"latest\"\nshellcheck = \"0.10.0\"\n")
	write(t, dir, ".tool-versions", "ruby 3.1.4\n")
	write(t, dir, "Dockerfile", "FROM python:3.13-slim-bookworm AS b\nFROM debian:bookworm\nFROM alpine\nFROM ${X}\nFROM ghcr.io/x/y:1\n")
	write(t, dir, "compose.yaml", "services:\n  db:\n    image: postgres:12.19\n")
	write(t, dir, ".github/workflows/ci.yml", "jobs:\n  a:\n    runs-on: ubuntu-20.04\n    strategy:\n      matrix:\n        os: [macos-13, ubuntu-latest]\n        include:\n          - windows-2019\n    steps:\n      - name: on ubuntu-22.04 # not a runner\n")
	write(t, dir, "pyproject.toml", "[project]\nrequires-python = \">=3.8\"\n")
	write(t, dir, "package.json", "{\n  \"engines\": {\n    \"node\": \">=16.0.0\"\n  }\n}\n")
	write(t, dir, "go.mod", "module x\n\ngo 1.21\n")
	write(t, dir, "app/llm.py", "MODEL = \"claude-3-opus-20240229\"\nOTHER = 'gpt-4-0613'\nprose = claude-3 in a comment\n")
	write(t, dir, "node_modules/x/llm.js", "'gpt-3.5-turbo'\n")
	write(t, dir, "test/fixtures/model.json", `{"m": "gpt-4"}`)

	refs, err := Scan(dir)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, r := range refs {
		got = append(got, string(r.Source)+":"+r.Product+"@"+r.Version)
	}
	want := []string{
		"runner:github-actions-runner-images@ubuntu-20.04",
		"runner:github-actions-runner-images@macos-13",
		"runner:github-actions-runner-images@windows-2019",
		"tool:ruby@3.1.4",
		"image:python@3.13",
		"image:debian@bookworm",
		"model:claude@claude-3-opus-20240229",
		"model:openai-api-models@gpt-4-0613",
		"image:postgresql@12.19",
		"constraint:go@1.21",
		"tool:python@3.9.18",
		"constraint:nodejs@16",
		"constraint:python@3.8",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("Scan() =\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}
