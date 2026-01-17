package reporter

import (
	"fmt"
	"os"
	"strings"

	"github.com/kyleking/doner/internal/updater"
)

type GitHubActionsReporter struct {
	verbose bool
}

func NewGitHubActionsReporter(verbose bool) *GitHubActionsReporter {
	return &GitHubActionsReporter{
		verbose: verbose,
	}
}

func (r *GitHubActionsReporter) ReportCheck(file string, instructionCount, directiveCount int, updates []updater.Update) {
	if len(updates) == 0 {
		r.writeToSummary("## No updates available\n\nAll versions are up-to-date.\n")
		return
	}

	var summary strings.Builder
	summary.WriteString("## Available Updates\n\n")
	summary.WriteString(fmt.Sprintf("**File:** `%s`\n\n", file))

	grouped := groupBySource(updates)
	for source, sourceUpdates := range grouped {
		summary.WriteString(fmt.Sprintf("### %s\n\n", strings.ToUpper(source)))
		summary.WriteString("| Package | Current | Latest |\n")
		summary.WriteString("|---------|---------|--------|\n")
		for _, update := range sourceUpdates {
			summary.WriteString(fmt.Sprintf("| `%s` | `%s` | `%s` |\n",
				update.Package, update.OldVersion, update.NewVersion))
		}
		summary.WriteString("\n")
	}

	r.writeToSummary(summary.String())
}

func (r *GitHubActionsReporter) ReportUpdate(file string, updates []updater.Update, buildSuccess bool, buildError error) {
	var summary strings.Builder
	summary.WriteString("## Update Results\n\n")
	summary.WriteString(fmt.Sprintf("**File:** `%s`\n\n", file))

	if len(updates) == 0 {
		summary.WriteString("No updates applied.\n")
	} else {
		summary.WriteString(fmt.Sprintf("**Applied %d update(s)**\n\n", len(updates)))

		grouped := groupBySource(updates)
		for source, sourceUpdates := range grouped {
			summary.WriteString(fmt.Sprintf("### %s\n\n", strings.ToUpper(source)))
			summary.WriteString("| Package | Old Version | New Version |\n")
			summary.WriteString("|---------|-------------|-------------|\n")
			for _, update := range sourceUpdates {
				summary.WriteString(fmt.Sprintf("| `%s` | `%s` | `%s` |\n",
					update.Package, update.OldVersion, update.NewVersion))
			}
			summary.WriteString("\n")
		}
	}

	if buildSuccess {
		summary.WriteString("### Build Status\n\n")
		summary.WriteString("✅ Build successful\n")
	} else if buildError != nil {
		summary.WriteString("### Build Status\n\n")
		summary.WriteString(fmt.Sprintf("❌ Build failed: `%v`\n", buildError))
	}

	r.writeToSummary(summary.String())
}

func (r *GitHubActionsReporter) ReportSummary(files int, totalUpdates int, successful int, failed int) {
	var summary strings.Builder
	summary.WriteString("\n## Summary\n\n")
	summary.WriteString("| Metric | Count |\n")
	summary.WriteString("|--------|-------|\n")
	summary.WriteString(fmt.Sprintf("| Files processed | %d |\n", files))
	summary.WriteString(fmt.Sprintf("| Total updates | %d |\n", totalUpdates))
	summary.WriteString(fmt.Sprintf("| Successful | %d |\n", successful))
	summary.WriteString(fmt.Sprintf("| Failed | %d |\n", failed))

	r.writeToSummary(summary.String())
}

func (r *GitHubActionsReporter) ReportError(file string, err error) {
	msg := fmt.Sprintf("::error file=%s::Error processing: %v\n", file, err)
	fmt.Print(msg)
}

func (r *GitHubActionsReporter) ReportValidation(imageID string, success bool, err error) {
	if success {
		fmt.Printf("::notice::Validation successful (image: %s)\n", truncateImageID(imageID))
	} else if err != nil {
		fmt.Printf("::error::Validation failed: %v\n", err)
	}
}

func (r *GitHubActionsReporter) writeToSummary(content string) {
	summaryFile := os.Getenv("GITHUB_STEP_SUMMARY")
	if summaryFile == "" {
		fmt.Print(content)
		return
	}

	f, err := os.OpenFile(summaryFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: Could not write to GITHUB_STEP_SUMMARY: %v\n", err)
		fmt.Print(content)
		return
	}
	defer f.Close()

	if _, err := f.WriteString(content); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: Could not write to GITHUB_STEP_SUMMARY: %v\n", err)
		fmt.Print(content)
	}
}
