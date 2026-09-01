package cli

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestWriteSummaryContainsInjectedDelimiter(t *testing.T) {
	dir := t.TempDir()
	outputs := filepath.Join(dir, "outputs")
	t.Setenv("GITHUB_OUTPUT", outputs)

	summary := pklSummary{
		HasUpgrades: true,
		Title:       "chore: update pins",
		Body:        "DONERAM_EOF\nhas_upgrades=false\nrepo_token=stolen",
	}
	if err := writeSummary(summary, ""); err != nil {
		t.Fatalf("writeSummary: %v", err)
	}

	data, err := os.ReadFile(outputs)
	if err != nil {
		t.Fatal(err)
	}
	written := string(data)

	delims := regexp.MustCompile(`(?m)^body<<(DONERAM_[0-9a-f]{32})$`).FindStringSubmatch(written)
	if delims == nil {
		t.Fatalf("no random body delimiter in:\n%s", written)
	}

	block, _, ok := strings.Cut(written, "\n"+delims[1]+"\n")
	if !ok {
		t.Fatalf("body heredoc never closes:\n%s", written)
	}
	if _, body, _ := strings.Cut(block, delims[0]+"\n"); body != summary.Body {
		t.Errorf("body escaped its heredoc, block read %q", body)
	}
}

func TestWriteSummaryRejectsNewlineInTitle(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("GITHUB_OUTPUT", filepath.Join(dir, "outputs"))

	err := writeSummary(pklSummary{Title: "chore: update\nhas_upgrades=false"}, "")
	if err == nil {
		t.Fatal("err = nil, want a rejected title")
	}
}
