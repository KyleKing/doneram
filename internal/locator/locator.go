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
	// Expect is the exact match count the site declares. Zero means "one
	// or more", for a pin in a file whose owner may legitimately repeat it
	// (a generated file a project extends).
	Expect int
	// Window is how many consecutive lines Pattern is matched against at
	// once, for a version tied to context on another line (a pre-commit
	// hook's rev under its repo URL). Zero or negative defaults to 1.
	// Pattern must anchor on text unique to its window (e.g. a specific
	// repo URL), or an occurrence can be double-counted across overlapping
	// windows.
	Window int
}

// ExpectsExactly reports whether the site declared a specific match count
// rather than "one or more".
func (l Locator) ExpectsExactly() bool {
	return l.Expect > 0
}

// WindowSize returns Window, defaulting to 1.
func (l Locator) WindowSize() int {
	if l.Window <= 0 {
		return 1
	}
	return l.Window
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

	window := l.WindowSize()

	var matches []Match
	for _, file := range files {
		content, err := os.ReadFile(file)
		if err != nil {
			return nil, fmt.Errorf("reading %s: %w", file, err)
		}

		lines := strings.Split(string(content), "\n")
		for i := 0; i+window <= len(lines); i++ {
			block := strings.Join(lines[i:i+window], "\n")
			for _, m := range re.FindAllStringSubmatch(block, -1) {
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
