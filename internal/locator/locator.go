// Package locator implements the regex-capture-and-patch primitive that
// resolves a version pin to a file location, independent of any particular
// file format. A Dockerfile directive and a pkl config site both compile
// down to a Locator.
package locator

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// Locator finds a version literal across a set of files.
type Locator struct {
	Glob     string // file path or glob this locator applies to
	Pattern  string // regex with exactly one capture group around the version
	Resolver string // resolver kind, e.g. "npm", "docker", "mise"
	Expect   int    // expected match count; zero or negative defaults to 1
}

// ExpectedCount returns Expect, defaulting to 1.
func (l Locator) ExpectedCount() int {
	if l.Expect <= 0 {
		return 1
	}
	return l.Expect
}

// Match is one capture-group occurrence found by Find.
type Match struct {
	File  string
	Line  int
	Value string
}

// Find globs l.Glob and returns every occurrence of l.Pattern's capture
// group across the matched files, in file then line order.
func Find(l Locator) ([]Match, error) {
	re, err := compileSingleCapture(l.Pattern)
	if err != nil {
		return nil, err
	}

	files, err := filepath.Glob(l.Glob)
	if err != nil {
		return nil, fmt.Errorf("globbing %q: %w", l.Glob, err)
	}

	var matches []Match
	for _, file := range files {
		content, err := os.ReadFile(file)
		if err != nil {
			return nil, fmt.Errorf("reading %s: %w", file, err)
		}

		for i, line := range strings.Split(string(content), "\n") {
			for _, m := range re.FindAllStringSubmatch(line, -1) {
				matches = append(matches, Match{File: file, Line: i + 1, Value: m[1]})
			}
		}
	}

	return matches, nil
}

func compileSingleCapture(pattern string) (*regexp.Regexp, error) {
	re, err := regexp.Compile(pattern)
	if err != nil {
		return nil, fmt.Errorf("compiling pattern %q: %w", pattern, err)
	}
	if re.NumSubexp() != 1 {
		return nil, fmt.Errorf("pattern %q must have exactly one capture group, got %d", pattern, re.NumSubexp())
	}
	return re, nil
}
