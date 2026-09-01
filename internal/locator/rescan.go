package locator

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// MismatchError reports a site whose actual match count disagreed with what
// it declared, carrying ranked candidates from a loosened rescan.
type MismatchError struct {
	Locator    Locator
	Got        int
	Want       int
	Candidates []Candidate
}

func (e *MismatchError) Error() string {
	want := fmt.Sprintf("%d", e.Want)
	if !e.Locator.ExpectsExactly() {
		want = "at least 1"
	}
	return fmt.Sprintf("%s: pattern matched %d time(s), want %s", e.Locator.Glob, e.Got, want)
}

// Candidate is a version-shaped literal found near the same context during a
// rescan, ranked by how closely its line matches that context.
type Candidate struct {
	File  string
	Line  int
	Value string
}

// CheckExpect fails with a *MismatchError, populated by Rescan, when the
// actual match count disagrees with what the site declared.
func CheckExpect(l Locator, matches []Match) error {
	want := l.Expect
	switch {
	case l.ExpectsExactly() && len(matches) == want:
		return nil
	case !l.ExpectsExactly() && len(matches) > 0:
		return nil
	}
	if want == 0 {
		want = 1
	}

	candidates, err := Rescan(l)
	if err != nil {
		candidates = nil
	}

	return &MismatchError{Locator: l, Got: len(matches), Want: want, Candidates: candidates}
}

var versionLiteral = regexp.MustCompile(`\d+(?:\.\d+){1,3}`)

// Rescan loosens l.Pattern to the literal text surrounding its capture
// group and returns every version-shaped literal on a line matching that
// context, so a moved pin shows up as a ranked candidate instead of a
// silent gap in the report.
func Rescan(l Locator) ([]Candidate, error) {
	context := contextBeforeCapture(l.Pattern)

	var contextRe *regexp.Regexp
	if context != "" {
		re, err := regexp.Compile(context)
		if err == nil {
			contextRe = re
		}
	}

	files, err := filepath.Glob(l.Glob)
	if err != nil {
		return nil, fmt.Errorf("globbing %q: %w", l.Glob, err)
	}

	var candidates []Candidate
	for _, file := range files {
		content, err := os.ReadFile(file)
		if err != nil {
			return nil, fmt.Errorf("reading %s: %w", file, err)
		}

		for i, line := range strings.Split(string(content), "\n") {
			if contextRe != nil && !contextRe.MatchString(line) {
				continue
			}
			for _, v := range versionLiteral.FindAllString(line, -1) {
				candidates = append(candidates, Candidate{File: file, Line: i + 1, Value: v})
			}
		}
	}

	return candidates, nil
}

// contextBeforeCapture returns the portion of pattern before its first
// unescaped capture group, which is usually the literal key text a version
// pin sits next to (e.g. `"golangci-lint" = "` in `"golangci-lint" = "([\d.]+)"`).
func contextBeforeCapture(pattern string) string {
	for i := 0; i < len(pattern); i++ {
		if pattern[i] != '(' {
			continue
		}
		if i > 0 && pattern[i-1] == '\\' {
			continue
		}
		return pattern[:i]
	}
	return ""
}
