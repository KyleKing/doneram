package locator

import (
	"fmt"
	"os"
)

// Patch rewrites every capture-group occurrence of pattern in file with
// newValue, leaving the rest of each match untouched, and returns how many
// occurrences it patched.
func Patch(file, pattern, newValue string) (int, error) {
	re, err := compileSingleCapture(pattern)
	if err != nil {
		return 0, err
	}

	content, err := os.ReadFile(file)
	if err != nil {
		return 0, fmt.Errorf("reading %s: %w", file, err)
	}

	count := 0
	patched := re.ReplaceAllFunc(content, func(match []byte) []byte {
		loc := re.FindSubmatchIndex(match)
		if len(loc) < 4 {
			return match
		}
		count++

		out := make([]byte, 0, len(match)+len(newValue))
		out = append(out, match[:loc[2]]...)
		out = append(out, []byte(newValue)...)
		out = append(out, match[loc[3]:]...)
		return out
	})

	if count == 0 {
		return 0, nil
	}

	info, err := os.Stat(file)
	mode := os.FileMode(0o644)
	if err == nil {
		mode = info.Mode()
	}

	if err := os.WriteFile(file, patched, mode); err != nil {
		return 0, fmt.Errorf("writing %s: %w", file, err)
	}

	return count, nil
}
