package reporter

import (
	"fmt"
	"strings"

	"github.com/kyleking/doner/internal/updater"
)

type OutputReporter interface {
	ReportCheck(file string, instructionCount, directiveCount int, updates []updater.Update)
	ReportUpdate(file string, updates []updater.Update, buildSuccess bool, buildError error)
	ReportSummary(files int, totalUpdates int, successful int, failed int)
	ReportError(file string, err error)
	ReportValidation(imageID string, success bool, err error)
}

// Reporter formats and displays update results
type Reporter struct {
	verbose bool
}

// NewReporter creates a new reporter
func NewReporter(verbose bool) *Reporter {
	return &Reporter{
		verbose: verbose,
	}
}

// ReportCheck displays check results (dry-run)
func (r *Reporter) ReportCheck(file string, instructionCount, directiveCount int, updates []updater.Update) {
	fmt.Printf("Parsed %s:\n", file)
	fmt.Printf("  Instructions: %d\n", instructionCount)
	fmt.Printf("  Directives:   %d\n", directiveCount)
	fmt.Println()

	if len(updates) == 0 {
		fmt.Println("No updates available. All versions are up-to-date.")
		return
	}

	grouped := groupBySource(updates)
	fmt.Println("Available updates:")
	for source, sourceUpdates := range grouped {
		fmt.Printf("\n  %s:\n", strings.ToUpper(source))
		for _, update := range sourceUpdates {
			fmt.Printf("    → %s:%s -> %s\n", update.Package, update.OldVersion, update.NewVersion)
		}
	}
}

// ReportUpdate displays update results with build validation
func (r *Reporter) ReportUpdate(file string, updates []updater.Update, buildSuccess bool, buildError error) {
	fmt.Printf("Updated %s:\n", file)

	if len(updates) == 0 {
		fmt.Println("  No updates applied.")
		return
	}

	grouped := groupBySource(updates)
	fmt.Printf("  Applied %d update(s):\n", len(updates))
	for source, sourceUpdates := range grouped {
		fmt.Printf("\n    %s:\n", strings.ToUpper(source))
		for _, update := range sourceUpdates {
			fmt.Printf("      → %s:%s -> %s\n", update.Package, update.OldVersion, update.NewVersion)
		}
	}
	fmt.Println()

	if buildSuccess {
		fmt.Println("  ✓ Build successful")
	} else if buildError != nil {
		fmt.Printf("  ✗ Build failed: %v\n", buildError)
	}
}

// ReportSummary displays a summary table of all updates
func (r *Reporter) ReportSummary(files int, totalUpdates int, successful int, failed int) {
	fmt.Println()
	fmt.Println("Summary:")
	fmt.Println(strings.Repeat("=", 50))
	fmt.Printf("  Files processed:    %d\n", files)
	fmt.Printf("  Total updates:      %d\n", totalUpdates)
	fmt.Printf("  Successful:         %d\n", successful)
	fmt.Printf("  Failed:             %d\n", failed)
	fmt.Println(strings.Repeat("=", 50))
}

// ReportError displays an error message
func (r *Reporter) ReportError(file string, err error) {
	fmt.Printf("Error processing %s: %v\n", file, err)
}

// ReportValidation displays validation results
func (r *Reporter) ReportValidation(imageID string, success bool, err error) {
	if success {
		fmt.Printf("  ✓ Validation successful (image: %s)\n", truncateImageID(imageID))
	} else if err != nil {
		fmt.Printf("  ✗ Validation failed: %v\n", err)
	}
}

func truncateImageID(imageID string) string {
	if strings.HasPrefix(imageID, "sha256:") {
		imageID = imageID[7:]
	}
	if len(imageID) > 12 {
		return imageID[:12]
	}
	return imageID
}

func groupBySource(updates []updater.Update) map[string][]updater.Update {
	grouped := make(map[string][]updater.Update)
	for _, update := range updates {
		source := update.Source
		if source == "" {
			source = "unknown"
		}
		grouped[source] = append(grouped[source], update)
	}
	return grouped
}

func NewOutputReporter(format string, verbose bool) OutputReporter {
	switch format {
	case "github-actions":
		return NewGitHubActionsReporter(verbose)
	case "json":
		return NewJSONReporter(verbose)
	default:
		return NewReporter(verbose)
	}
}
