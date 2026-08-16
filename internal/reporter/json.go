package reporter

import (
	"encoding/json"
	"fmt"

	"github.com/kyleking/doneram/internal/updater"
)

type JSONReporter struct {
	verbose bool
}

func NewJSONReporter(verbose bool) *JSONReporter {
	return &JSONReporter{
		verbose: verbose,
	}
}

type CheckResult struct {
	File             string           `json:"file"`
	InstructionCount int              `json:"instruction_count"`
	DirectiveCount   int              `json:"directive_count"`
	Updates          []updater.Update `json:"updates"`
	UpdateCount      int              `json:"update_count"`
}

type UpdateResult struct {
	File         string           `json:"file"`
	Updates      []updater.Update `json:"updates"`
	UpdateCount  int              `json:"update_count"`
	BuildSuccess bool             `json:"build_success"`
	BuildError   string           `json:"build_error,omitempty"`
}

type SummaryResult struct {
	FilesProcessed int `json:"files_processed"`
	TotalUpdates   int `json:"total_updates"`
	Successful     int `json:"successful"`
	Failed         int `json:"failed"`
}

func (r *JSONReporter) ReportCheck(file string, instructionCount, directiveCount int, updates []updater.Update) {
	result := CheckResult{
		File:             file,
		InstructionCount: instructionCount,
		DirectiveCount:   directiveCount,
		Updates:          updates,
		UpdateCount:      len(updates),
	}

	data, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		fmt.Printf("{\"error\": \"Failed to marshal JSON: %v\"}\n", err)
		return
	}
	fmt.Println(string(data))
}

func (r *JSONReporter) ReportUpdate(file string, updates []updater.Update, buildSuccess bool, buildError error) {
	result := UpdateResult{
		File:         file,
		Updates:      updates,
		UpdateCount:  len(updates),
		BuildSuccess: buildSuccess,
	}
	if buildError != nil {
		result.BuildError = buildError.Error()
	}

	data, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		fmt.Printf("{\"error\": \"Failed to marshal JSON: %v\"}\n", err)
		return
	}
	fmt.Println(string(data))
}

func (r *JSONReporter) ReportSummary(files int, totalUpdates int, successful int, failed int) {
	result := SummaryResult{
		FilesProcessed: files,
		TotalUpdates:   totalUpdates,
		Successful:     successful,
		Failed:         failed,
	}

	data, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		fmt.Printf("{\"error\": \"Failed to marshal JSON: %v\"}\n", err)
		return
	}
	fmt.Println(string(data))
}

func (r *JSONReporter) ReportError(file string, err error) {
	result := map[string]string{
		"error": err.Error(),
		"file":  file,
	}
	data, _ := json.MarshalIndent(result, "", "  ")
	fmt.Println(string(data))
}

func (r *JSONReporter) ReportValidation(imageID string, success bool, err error) {
	result := map[string]interface{}{
		"image_id": imageID,
		"success":  success,
	}
	if err != nil {
		result["error"] = err.Error()
	}
	data, _ := json.MarshalIndent(result, "", "  ")
	fmt.Println(string(data))
}
