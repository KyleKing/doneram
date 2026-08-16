package updater

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/kyleking/doneram/internal/parser"
)

// Update represents a version update for a package
type Update struct {
	Package    string
	Source     string // "docker", "ghcr", etc.
	OldVersion string
	NewVersion string
	Line       int
}

// Updater modifies Dockerfile content with version updates
type Updater struct {
	content string
	lines   []string
}

// NewUpdater creates a new Dockerfile updater
func NewUpdater(content string) *Updater {
	return &Updater{
		content: content,
		lines:   strings.Split(content, "\n"),
	}
}

// Apply applies a list of updates to the Dockerfile
func (u *Updater) Apply(updates []Update) error {
	for _, update := range updates {
		if err := u.applyUpdate(update); err != nil {
			return fmt.Errorf("applying update at line %d: %w", update.Line, err)
		}
	}
	return nil
}

// applyUpdate applies a single update to the specified line
func (u *Updater) applyUpdate(update Update) error {
	if update.Line < 1 || update.Line > len(u.lines) {
		return fmt.Errorf("line %d out of range", update.Line)
	}

	lineIdx := update.Line - 1
	line := u.lines[lineIdx]

	// Replace the old version with the new version
	// Handle both FROM and COPY --from= instructions
	updated := strings.ReplaceAll(line, ":"+update.OldVersion, ":"+update.NewVersion)

	if updated == line {
		return fmt.Errorf("version %s not found in line", update.OldVersion)
	}

	u.lines[lineIdx] = updated
	return nil
}

// Content returns the updated Dockerfile content
func (u *Updater) Content() string {
	return strings.Join(u.lines, "\n")
}

// UpdateFromInstructions creates updates from parser instructions and resolved versions
func UpdateFromInstructions(instructions []parser.Instruction, directives map[int]*parser.Directive, resolved map[int]string) []Update {
	var updates []Update

	for _, instr := range instructions {
		directive := directives[instr.Line-1]
		if directive == nil || directive.Ignore {
			continue
		}

		newVersion := resolved[instr.Line]
		if newVersion == "" {
			continue
		}

		// Extract current version from instruction
		var currentVersion string
		var imageName string

		if instr.Command == "FROM" {
			imageName, currentVersion = parseFromInstruction(instr.Args)
		} else if instr.Command == "COPY" && strings.Contains(instr.Args, "--from=") {
			imageName, currentVersion = parseCopyFromInstruction(instr.Args)
		}

		if currentVersion == "" || currentVersion == newVersion {
			continue
		}

		source := "docker"
		if strings.Contains(imageName, "ghcr.io") {
			source = "ghcr"
		}

		updates = append(updates, Update{
			Package:    imageName,
			Source:     source,
			OldVersion: currentVersion,
			NewVersion: newVersion,
			Line:       instr.Line,
		})
	}

	return updates
}

var imageRegex = regexp.MustCompile(`^([^:]+):(.+)$`)
var copyFromRegex = regexp.MustCompile(`--from=([^\s]+)`)

func parseFromInstruction(args string) (string, string) {
	image := strings.TrimSpace(args)
	// Remove AS alias if present
	if idx := strings.Index(image, " AS "); idx != -1 {
		image = image[:idx]
	}
	if idx := strings.Index(image, " as "); idx != -1 {
		image = image[:idx]
	}

	matches := imageRegex.FindStringSubmatch(strings.TrimSpace(image))
	if matches == nil {
		return "", ""
	}
	return matches[1], matches[2]
}

func parseCopyFromInstruction(args string) (string, string) {
	matches := copyFromRegex.FindStringSubmatch(args)
	if matches == nil {
		return "", ""
	}

	image := matches[1]
	imageMatches := imageRegex.FindStringSubmatch(image)
	if imageMatches == nil {
		return "", ""
	}
	return imageMatches[1], imageMatches[2]
}
