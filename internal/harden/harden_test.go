package harden

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type fakeResolver struct{ calls int }

func (f *fakeResolver) PinAction(_ context.Context, repo, ref string) (string, error) {
	f.calls++
	if repo == "gone/away" {
		return "", errors.New("not found")
	}
	return strings.Repeat("a", 40) + " # " + ref + ".2.1", nil
}

func (f *fakeResolver) PinTool(_ context.Context, tool, ref string) (string, error) {
	f.calls++
	if tool == "weird" {
		return "nightly", nil
	}
	return "22.11.0", nil
}

func (f *fakeResolver) ImageDigest(_ context.Context, image string) (string, error) {
	f.calls++
	return "sha256:" + strings.Repeat("d", 64), nil
}

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

const workflow = `on: push
jobs:
  a:
    steps:
      - uses: actions/checkout@v4
      - uses: "actions/setup-go@v5" # setup
      - uses: github/codeql-action/init@v3 # v3
      - uses: actions/cache@` + "0123456789abcdef0123456789abcdef01234567" + ` # v4.1.0
      - uses: ./local
      - uses: docker://alpine:3
      - uses: owner/tool@main
      # - uses: actions/checkout@v3
      - uses: actions/checkout@v4
`

func TestScanResolveApply(t *testing.T) {
	dir := t.TempDir()
	wf := write(t, dir, ".github/workflows/ci.yml", workflow)
	mise := write(t, dir, "mise.toml", "[tools]\nnode = \"22\"\ngo = \"1.23.4\"\npython = \"latest\"\nweird = \"lts\"\n\n[env]\nX = \"1\"\n")
	tv := write(t, dir, ".tool-versions", "ruby 3\nnodejs 20.1.0\n")

	findings, err := Scan(dir)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, f := range findings {
		got = append(got, string(f.Kind)+":"+f.Name+"@"+f.Ref)
	}
	want := []string{
		"action:actions/checkout@v4", "action:actions/setup-go@v5", "action:github/codeql-action@v3",
		"action:owner/tool@main", "action:actions/checkout@v4",
		"tool:node@22", "tool:python@latest", "tool:weird@lts", "tool:ruby@3",
	}
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Fatalf("Scan() =\n%v\nwant\n%v", got, want)
	}

	r := &fakeResolver{}
	Resolve(context.Background(), findings, r)
	if r.calls != 7 {
		t.Errorf("resolver called %d times, want 7 (repeats memoized, branch never asked)", r.calls)
	}
	for _, f := range findings {
		switch f.Name {
		case "owner/tool", "weird":
			if f.Err == nil {
				t.Errorf("%s: want an error", f.Name)
			}
		default:
			if f.Err != nil {
				t.Errorf("%s: %v", f.Name, f.Err)
			}
		}
	}

	n, err := Apply(findings)
	if err != nil {
		t.Fatal(err)
	}
	if n != 7 {
		t.Errorf("Apply() changed %d, want 7", n)
	}

	sha := strings.Repeat("a", 40)
	data, _ := os.ReadFile(wf)
	for _, line := range []string{
		"- uses: actions/checkout@" + sha + " # v4.2.1",
		"- uses: actions/setup-go@" + sha + " # v5.2.1 (setup)",
		"- uses: github/codeql-action/init@" + sha + " # v3.2.1",
		"- uses: owner/tool@main",
		"# - uses: actions/checkout@v3",
	} {
		if !strings.Contains(string(data), line) {
			t.Errorf("workflow missing %q:\n%s", line, data)
		}
	}
	if strings.Contains(string(data), "@v4\n") {
		t.Errorf("a repeated use was left floating:\n%s", data)
	}

	data, _ = os.ReadFile(mise)
	if want := "[tools]\nnode = \"22.11.0\"\ngo = \"1.23.4\"\npython = \"22.11.0\"\nweird = \"lts\"\n"; !strings.HasPrefix(string(data), want) {
		t.Errorf("mise.toml =\n%s", data)
	}
	data, _ = os.ReadFile(tv)
	if string(data) != "ruby 22.11.0\nnodejs 20.1.0\n" {
		t.Errorf(".tool-versions =\n%s", data)
	}
}

func TestApplyRefusesAChangedLine(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "mise.toml", "[tools]\nnode = \"22\"\n")
	findings, err := Scan(dir)
	if err != nil {
		t.Fatal(err)
	}
	Resolve(context.Background(), findings, &fakeResolver{})
	write(t, dir, "mise.toml", "[tools]\nnode = \"24.1.0\"\n")
	if _, err := Apply(findings); err == nil {
		t.Error("Apply() over a changed line succeeded, want an error")
	}
}
