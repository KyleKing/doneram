package reporter

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/kyleking/doner/internal/updater"
)

func TestJSONReporter_ReportCheck(t *testing.T) {
	r := NewJSONReporter(false)
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

	var result CheckResult
	if err := json.Unmarshal([]byte(output), &result); err != nil {
		t.Fatalf("failed to parse JSON: %v", err)
	}

	if result.File != "Dockerfile" {
		t.Errorf("File = %s, want Dockerfile", result.File)
	}
	if result.InstructionCount != 5 {
		t.Errorf("InstructionCount = %d, want 5", result.InstructionCount)
	}
	if result.DirectiveCount != 2 {
		t.Errorf("DirectiveCount = %d, want 2", result.DirectiveCount)
	}
	if result.UpdateCount != 1 {
		t.Errorf("UpdateCount = %d, want 1", result.UpdateCount)
	}
	if len(result.Updates) != 1 {
		t.Errorf("len(Updates) = %d, want 1", len(result.Updates))
	}
}

func TestJSONReporter_ReportCheck_NoUpdates(t *testing.T) {
	r := NewJSONReporter(false)

	output := captureOutput(func() {
		r.ReportCheck("Dockerfile", 5, 2, []updater.Update{})
	})

	var result CheckResult
	if err := json.Unmarshal([]byte(output), &result); err != nil {
		t.Fatalf("failed to parse JSON: %v", err)
	}

	if result.UpdateCount != 0 {
		t.Errorf("UpdateCount = %d, want 0", result.UpdateCount)
	}
}

func TestJSONReporter_ReportUpdate_Success(t *testing.T) {
	r := NewJSONReporter(false)
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

	var result UpdateResult
	if err := json.Unmarshal([]byte(output), &result); err != nil {
		t.Fatalf("failed to parse JSON: %v", err)
	}

	if result.File != "Dockerfile" {
		t.Errorf("File = %s, want Dockerfile", result.File)
	}
	if result.UpdateCount != 1 {
		t.Errorf("UpdateCount = %d, want 1", result.UpdateCount)
	}
	if !result.BuildSuccess {
		t.Error("BuildSuccess should be true")
	}
	if result.BuildError != "" {
		t.Errorf("BuildError should be empty, got %s", result.BuildError)
	}
}

func TestJSONReporter_ReportUpdate_BuildError(t *testing.T) {
	r := NewJSONReporter(false)
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

	var result UpdateResult
	if err := json.Unmarshal([]byte(output), &result); err != nil {
		t.Fatalf("failed to parse JSON: %v", err)
	}

	if result.BuildSuccess {
		t.Error("BuildSuccess should be false")
	}
	if result.BuildError != "build failed" {
		t.Errorf("BuildError = %s, want 'build failed'", result.BuildError)
	}
}

func TestJSONReporter_ReportSummary(t *testing.T) {
	r := NewJSONReporter(false)

	output := captureOutput(func() {
		r.ReportSummary(5, 10, 4, 1)
	})

	var result SummaryResult
	if err := json.Unmarshal([]byte(output), &result); err != nil {
		t.Fatalf("failed to parse JSON: %v", err)
	}

	if result.FilesProcessed != 5 {
		t.Errorf("FilesProcessed = %d, want 5", result.FilesProcessed)
	}
	if result.TotalUpdates != 10 {
		t.Errorf("TotalUpdates = %d, want 10", result.TotalUpdates)
	}
	if result.Successful != 4 {
		t.Errorf("Successful = %d, want 4", result.Successful)
	}
	if result.Failed != 1 {
		t.Errorf("Failed = %d, want 1", result.Failed)
	}
}

func TestJSONReporter_ReportError(t *testing.T) {
	r := NewJSONReporter(false)

	output := captureOutput(func() {
		r.ReportError("Dockerfile", errors.New("test error"))
	})

	var result map[string]string
	if err := json.Unmarshal([]byte(output), &result); err != nil {
		t.Fatalf("failed to parse JSON: %v", err)
	}

	if result["error"] != "test error" {
		t.Errorf("error = %s, want 'test error'", result["error"])
	}
	if result["file"] != "Dockerfile" {
		t.Errorf("file = %s, want Dockerfile", result["file"])
	}
}

func TestJSONReporter_ReportValidation_Success(t *testing.T) {
	r := NewJSONReporter(false)

	output := captureOutput(func() {
		r.ReportValidation("sha256:abc123", true, nil)
	})

	var result map[string]interface{}
	if err := json.Unmarshal([]byte(output), &result); err != nil {
		t.Fatalf("failed to parse JSON: %v", err)
	}

	if result["image_id"] != "sha256:abc123" {
		t.Errorf("image_id = %v, want sha256:abc123", result["image_id"])
	}
	if result["success"] != true {
		t.Error("success should be true")
	}
}

func TestJSONReporter_ReportValidation_Error(t *testing.T) {
	r := NewJSONReporter(false)

	output := captureOutput(func() {
		r.ReportValidation("sha256:abc123", false, errors.New("validation failed"))
	})

	var result map[string]interface{}
	if err := json.Unmarshal([]byte(output), &result); err != nil {
		t.Fatalf("failed to parse JSON: %v", err)
	}

	if result["success"] != false {
		t.Error("success should be false")
	}
	if result["error"] != "validation failed" {
		t.Errorf("error = %v, want 'validation failed'", result["error"])
	}
}

func TestNewJSONReporter(t *testing.T) {
	r := NewJSONReporter(true)
	if r == nil {
		t.Error("NewJSONReporter returned nil")
	}
	if !r.verbose {
		t.Error("verbose should be true")
	}
}
