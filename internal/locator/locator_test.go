package locator

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func writeFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile(%q): %v", path, err)
	}
	return path
}

func TestFindSingleMatch(t *testing.T) {
	dir := t.TempDir()
	path := writeFile(t, dir, "conf.toml", "actionlint = \"1.7.12\"\n")

	matches, err := Find(Locator{Glob: path, Pattern: `actionlint = "([\d.]+)"`})
	if err != nil {
		t.Fatalf("Find: %v", err)
	}
	if len(matches) != 1 || matches[0].Value != "1.7.12" || matches[0].Line != 1 {
		t.Fatalf("matches = %+v, want one match of 1.7.12 on line 1", matches)
	}
}

func TestFindAcrossGlob(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "a.toml", "hk = \"1.0.0\"\n")
	writeFile(t, dir, "b.toml", "hk = \"1.0.0\"\n")

	matches, err := Find(Locator{Glob: filepath.Join(dir, "*.toml"), Pattern: `hk = "([\d.]+)"`})
	if err != nil {
		t.Fatalf("Find: %v", err)
	}
	if len(matches) != 2 {
		t.Fatalf("matches = %+v, want 2", matches)
	}
}

func TestFindRejectsPatternWithoutExactlyOneCapture(t *testing.T) {
	dir := t.TempDir()
	path := writeFile(t, dir, "conf.toml", "actionlint = \"1.7.12\"\n")

	if _, err := Find(Locator{Glob: path, Pattern: `actionlint = "[\d.]+"`}); err == nil {
		t.Error("Find should reject a pattern with no capture group")
	}
	if _, err := Find(Locator{Glob: path, Pattern: `(actionlint) = "([\d.]+)"`}); err == nil {
		t.Error("Find should reject a pattern with two capture groups")
	}
}

func TestCheckExpectMismatchIncludesCandidates(t *testing.T) {
	dir := t.TempDir()
	path := writeFile(t, dir, "conf.toml", "\"golangci-lint\" = \"v2.12.2\"\n")

	l := Locator{Glob: path, Pattern: `"golangci-lint" = "([\d.]+)"`}
	matches, err := Find(l)
	if err != nil {
		t.Fatalf("Find: %v", err)
	}

	err = CheckExpect(l, matches)
	if err == nil {
		t.Fatal("CheckExpect should fail when the quoted key doesn't match")
	}

	var mismatch *MismatchError
	if !errors.As(err, &mismatch) {
		t.Fatalf("err = %v, want *MismatchError", err)
	}
	if mismatch.Got != 0 || mismatch.Want != 1 {
		t.Errorf("mismatch = %+v, want Got=0 Want=1", mismatch)
	}
	if len(mismatch.Candidates) != 1 || mismatch.Candidates[0].Value != "2.12.2" {
		t.Errorf("Candidates = %+v, want one candidate of 2.12.2", mismatch.Candidates)
	}
}

func TestCheckExpectAgreesByDefault(t *testing.T) {
	dir := t.TempDir()
	path := writeFile(t, dir, "conf.toml", "jq = \"1.7.1\"\n")

	l := Locator{Glob: path, Pattern: `jq = "([\d.]+)"`}
	matches, err := Find(l)
	if err != nil {
		t.Fatalf("Find: %v", err)
	}
	if err := CheckExpect(l, matches); err != nil {
		t.Errorf("CheckExpect: %v", err)
	}
}

func TestPatchRewritesEveryOccurrence(t *testing.T) {
	dir := t.TempDir()
	path := writeFile(t, dir, "hk.pkl", "download/v1.0.0/hk@\nhk@1.0.0#\n")

	count, err := Patch(path, `download/v([\d.]+)/hk@`, "1.1.0")
	if err != nil {
		t.Fatalf("Patch: %v", err)
	}
	if count != 1 {
		t.Fatalf("count = %d, want 1", count)
	}

	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if got := string(content); got != "download/v1.1.0/hk@\nhk@1.0.0#\n" {
		t.Errorf("content = %q", got)
	}
}

func TestPatchNoMatchLeavesFileUntouched(t *testing.T) {
	dir := t.TempDir()
	path := writeFile(t, dir, "conf.toml", "jq = \"1.7.1\"\n")

	count, err := Patch(path, `missing = "([\d.]+)"`, "9.9.9")
	if err != nil {
		t.Fatalf("Patch: %v", err)
	}
	if count != 0 {
		t.Errorf("count = %d, want 0", count)
	}

	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(content) != "jq = \"1.7.1\"\n" {
		t.Errorf("content changed: %q", string(content))
	}
}
