package cli

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunExplain(t *testing.T) {
	dir := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", dir, "-c", "user.email=t@t", "-c", "user.name=t"}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	write := func(name, body string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(".doneram.pkl", `discover = false
validate = "echo widget 2 broke the build; exit 1"
tools {
  ["widget"] {
    resolver = "github-release"
    resolverName = "acme/widget"
    sites { new { file = "versions.txt"; pattern = #"widget=([\d.]+)"# } }
  }
}
`)
	write("versions.txt", "widget=1.4.0\n")
	run("init", "-q", "-b", "main")
	run("add", "-A")
	run("commit", "-qm", "base")
	run("switch", "-qc", "doneram/failing-widget")
	write("versions.txt", "widget=2.0.0\n")
	run("commit", "-qam", "bump")

	var out strings.Builder
	if err := runExplain(context.Background(), &out, dir, "widget", "", true); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	for _, want := range []string{
		"moves widget from `1.4.0` to `2.0.0` (against `main`)",
		"`versions.txt:1` `2.0.0`",
		"`echo widget 2 broke the build; exit 1` fails",
		"widget 2 broke the build",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("output lacks %q:\n%s", want, got)
		}
	}

	if err := runExplain(context.Background(), &out, dir, "nope", "", false); err == nil {
		t.Error("unknown tool explained")
	}
}
