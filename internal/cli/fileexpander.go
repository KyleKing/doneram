package cli

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// expandFiles expands file patterns (including globs) into a deduplicated list of file paths.
// Supports simple globs (*.Dockerfile) and recursive patterns (**/Dockerfile).
// Returns error if no files match any pattern.
func expandFiles(patterns []string) ([]string, error) {
	if len(patterns) == 0 {
		return []string{"Dockerfile"}, nil
	}

	seen := make(map[string]bool)
	var result []string

	for _, pattern := range patterns {
		var matches []string
		var err error

		if strings.Contains(pattern, "**") {
			matches, err = expandRecursive(pattern)
		} else {
			matches, err = filepath.Glob(pattern)
			if err == nil && len(matches) == 0 {
				// Pattern didn't match - check if it's an exact path
				if fileExists(pattern) {
					matches = []string{pattern}
				}
			}
		}

		if err != nil {
			return nil, fmt.Errorf("expanding pattern %q: %w", pattern, err)
		}

		for _, match := range matches {
			absPath, err := filepath.Abs(match)
			if err != nil {
				absPath = match
			}
			if !seen[absPath] {
				seen[absPath] = true
				result = append(result, match)
			}
		}
	}

	if len(result) == 0 {
		return nil, fmt.Errorf("no files matched patterns: %v", patterns)
	}

	return result, nil
}

// expandRecursive handles patterns containing ** (recursive glob).
// Splits pattern into base directory and match pattern.
func expandRecursive(pattern string) ([]string, error) {
	// Find the first ** in the pattern
	parts := strings.Split(pattern, "**")
	if len(parts) != 2 {
		return nil, fmt.Errorf("invalid recursive pattern (multiple **): %s", pattern)
	}

	baseDir := parts[0]
	if baseDir == "" {
		baseDir = "."
	} else {
		baseDir = strings.TrimSuffix(baseDir, "/")
	}

	suffix := strings.TrimPrefix(parts[1], "/")

	var matches []string
	err := filepath.WalkDir(baseDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // Continue on errors
		}

		if d.IsDir() {
			return nil
		}

		// Match suffix pattern
		if suffix == "" || strings.HasSuffix(path, suffix) {
			matches = append(matches, path)
		}

		return nil
	})

	if err != nil {
		return nil, fmt.Errorf("walking directory %q: %w", baseDir, err)
	}

	return matches, nil
}

// fileExists checks if a file exists at the given path.
func fileExists(path string) bool {
	info, err := os.Lstat(path)
	if err != nil {
		return false
	}
	return !info.IsDir()
}
