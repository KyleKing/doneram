package reporter

import (
	"errors"
	"strings"
	"testing"

	"github.com/kyleking/doner/internal/updater"
)

func TestGitHubActionsReporter_ReportCheck_NoUpdates(t *testing.T) {
	r := NewGitHubActionsReporter(false)

	output := captureOutput(func() {
		r.ReportCheck("Dockerfile", 5, 2, []updater.Update{})
	})

	if !strings.Contains(output, "No updates available") {
		t.Error("output should indicate no updates")
	}
}

func TestGitHubActionsReporter_ReportCheck_WithUpdates(t *testing.T) {
	r := NewGitHubActionsReporter(false)
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

	if !strings.Contains(output, "Available Updates") {
		t.Error("output should contain 'Available Updates'")
	}
	if !strings.Contains(output, "Dockerfile") {
		t.Error("output should contain file name")
	}
	if !strings.Contains(output, "python") {
		t.Error("output should contain package name")
	}
	if !strings.Contains(output, "3.11.0") {
		t.Error("output should contain old version")
	}
	if !strings.Contains(output, "3.11.5") {
		t.Error("output should contain new version")
	}
	if !strings.Contains(output, "DOCKERHUB") {
		t.Error("output should contain source (DOCKERHUB)")
	}
}

func TestGitHubActionsReporter_ReportUpdate_NoUpdates(t *testing.T) {
	r := NewGitHubActionsReporter(false)

	output := captureOutput(func() {
		r.ReportUpdate("Dockerfile", []updater.Update{}, false, nil)
	})

	if !strings.Contains(output, "Update Results") {
		t.Error("output should contain 'Update Results'")
	}
	if !strings.Contains(output, "No updates applied") {
		t.Error("output should indicate no updates")
	}
}

func TestGitHubActionsReporter_ReportUpdate_Success(t *testing.T) {
	r := NewGitHubActionsReporter(false)
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
	if !strings.Contains(output, "python") {
		t.Error("output should contain package name")
	}
}

func TestGitHubActionsReporter_ReportUpdate_BuildError(t *testing.T) {
	r := NewGitHubActionsReporter(false)
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
	if !strings.Contains(output, "build failed") {
		t.Error("output should contain error message")
	}
}

func TestGitHubActionsReporter_ReportSummary(t *testing.T) {
	r := NewGitHubActionsReporter(false)

	output := captureOutput(func() {
		r.ReportSummary(5, 10, 4, 1)
	})

	if !strings.Contains(output, "Summary") {
		t.Error("output should contain 'Summary'")
	}
	if !strings.Contains(output, "Files processed") {
		t.Error("output should contain 'Files processed'")
	}
	if !strings.Contains(output, "5") {
		t.Error("output should contain files count")
	}
	if !strings.Contains(output, "10") {
		t.Error("output should contain updates count")
	}
}

func TestGitHubActionsReporter_ReportError(t *testing.T) {
	r := NewGitHubActionsReporter(false)

	output := captureOutput(func() {
		r.ReportError("Dockerfile", errors.New("test error"))
	})

	if !strings.Contains(output, "::error") {
		t.Error("output should contain GitHub Actions error annotation")
	}
	if !strings.Contains(output, "Dockerfile") {
		t.Error("output should contain file name")
	}
	if !strings.Contains(output, "test error") {
		t.Error("output should contain error message")
	}
}

func TestGitHubActionsReporter_ReportValidation_Success(t *testing.T) {
	r := NewGitHubActionsReporter(false)

	output := captureOutput(func() {
		r.ReportValidation("sha256:abc123", true, nil)
	})

	if !strings.Contains(output, "::notice") {
		t.Error("output should contain GitHub Actions notice annotation")
	}
	if !strings.Contains(output, "Validation successful") {
		t.Error("output should indicate success")
	}
}

func TestGitHubActionsReporter_ReportValidation_Error(t *testing.T) {
	r := NewGitHubActionsReporter(false)

	output := captureOutput(func() {
		r.ReportValidation("sha256:abc123", false, errors.New("validation failed"))
	})

	if !strings.Contains(output, "::error") {
		t.Error("output should contain GitHub Actions error annotation")
	}
	if !strings.Contains(output, "Validation failed") {
		t.Error("output should indicate failure")
	}
}

func TestNewGitHubActionsReporter(t *testing.T) {
	r := NewGitHubActionsReporter(true)
	if r == nil {
		t.Fatal("NewGitHubActionsReporter returned nil")
	}
	if !r.verbose {
		t.Error("verbose should be true")
	}
}
