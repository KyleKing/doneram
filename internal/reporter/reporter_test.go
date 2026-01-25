package reporter

import (
	"bytes"
	"errors"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/kyleking/doner/internal/updater"
)

func captureOutput(f func()) string {
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	f()

	_ = w.Close()
	os.Stdout = old

	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	return buf.String()
}

func TestReporter_ReportCheck_NoUpdates(t *testing.T) {
	r := NewReporter(false)
	output := captureOutput(func() {
		r.ReportCheck("Dockerfile", 5, 2, []updater.Update{})
	})

	if !strings.Contains(output, "Dockerfile") {
		t.Error("output should contain file name")
	}
	if !strings.Contains(output, "No updates available") {
		t.Error("output should indicate no updates")
	}
}

func TestReporter_ReportCheck_WithUpdates(t *testing.T) {
	r := NewReporter(false)
	updates := []updater.Update{
		{
			Package:    "python",
			OldVersion: "3.11.0",
			NewVersion: "3.11.5",
			Source:     "dockerhub",
		},
	}

	output := captureOutput(func() {
		r.ReportCheck("Dockerfile", 5, 2, updates)
	})

	if !strings.Contains(output, "python") {
		t.Error("output should contain package name")
	}
	if !strings.Contains(output, "3.11.0") {
		t.Error("output should contain old version")
	}
	if !strings.Contains(output, "3.11.5") {
		t.Error("output should contain new version")
	}
}

func TestReporter_ReportUpdate_Success(t *testing.T) {
	r := NewReporter(false)
	updates := []updater.Update{
		{
			Package:    "python",
			OldVersion: "3.11.0",
			NewVersion: "3.11.5",
			Source:     "dockerhub",
		},
	}

	output := captureOutput(func() {
		r.ReportUpdate("Dockerfile", updates, true, nil)
	})

	if !strings.Contains(output, "Build successful") {
		t.Error("output should indicate build success")
	}
}

func TestReporter_ReportUpdate_BuildError(t *testing.T) {
	r := NewReporter(false)
	updates := []updater.Update{
		{
			Package:    "python",
			OldVersion: "3.11.0",
			NewVersion: "3.11.5",
			Source:     "dockerhub",
		},
	}

	output := captureOutput(func() {
		r.ReportUpdate("Dockerfile", updates, false, errors.New("build failed"))
	})

	if !strings.Contains(output, "Build failed") {
		t.Error("output should indicate build failure")
	}
}

func TestReporter_ReportSummary(t *testing.T) {
	r := NewReporter(false)

	output := captureOutput(func() {
		r.ReportSummary(5, 10, 4, 1)
	})

	if !strings.Contains(output, "Summary") {
		t.Error("output should contain summary header")
	}
	if !strings.Contains(output, "5") {
		t.Error("output should contain files count")
	}
	if !strings.Contains(output, "10") {
		t.Error("output should contain updates count")
	}
}

func TestReporter_ReportError(t *testing.T) {
	r := NewReporter(false)

	output := captureOutput(func() {
		r.ReportError("Dockerfile", errors.New("test error"))
	})

	if !strings.Contains(output, "Error") {
		t.Error("output should contain error message")
	}
	if !strings.Contains(output, "test error") {
		t.Error("output should contain error details")
	}
}

func TestTruncateImageID(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"sha256:0123456789abcdef0123456789abcdef", "0123456789ab"},
		{"0123456789abcdef", "0123456789ab"},
		{"short", "short"},
		{"", ""},
	}

	for _, tt := range tests {
		got := truncateImageID(tt.input)
		if got != tt.want {
			t.Errorf("truncateImageID(%s) = %s, want %s", tt.input, got, tt.want)
		}
	}
}

func TestGroupBySource(t *testing.T) {
	updates := []updater.Update{
		{Package: "python", Source: "dockerhub"},
		{Package: "requests", Source: "pypi"},
		{Package: "alpine", Source: "dockerhub"},
		{Package: "express", Source: ""},
	}

	grouped := groupBySource(updates)

	if len(grouped) != 3 {
		t.Errorf("expected 3 groups, got %d", len(grouped))
	}

	if len(grouped["dockerhub"]) != 2 {
		t.Errorf("expected 2 dockerhub updates, got %d", len(grouped["dockerhub"]))
	}

	if len(grouped["pypi"]) != 1 {
		t.Errorf("expected 1 pypi update, got %d", len(grouped["pypi"]))
	}

	if len(grouped["unknown"]) != 1 {
		t.Errorf("expected 1 unknown update, got %d", len(grouped["unknown"]))
	}
}

func TestNewOutputReporter(t *testing.T) {
	tests := []struct {
		format   string
		wantType string
	}{
		{"stdout", "*reporter.Reporter"},
		{"github-actions", "*reporter.GitHubActionsReporter"},
		{"json", "*reporter.JSONReporter"},
		{"unknown", "*reporter.Reporter"},
	}

	for _, tt := range tests {
		t.Run(tt.format, func(t *testing.T) {
			r := NewOutputReporter(tt.format, false)
			if r == nil {
				t.Error("NewOutputReporter returned nil")
			}
		})
	}
}
