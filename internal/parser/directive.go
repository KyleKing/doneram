package parser

import (
	"regexp"
	"strings"
)

type Directive struct {
	Line     int
	Raw      string
	Packages []PackageDirective
	Ignore   bool
}

type PackageDirective struct {
	Name    string
	Pattern *VersionPattern
	Ignore  bool
}

var directiveRegex = regexp.MustCompile(`^\s*#\s*doneram:\s*(.+)$`)

func ParseDirective(line string, lineNum int) *Directive {
	matches := directiveRegex.FindStringSubmatch(line)
	if matches == nil {
		return nil
	}

	raw := strings.TrimSpace(matches[1])

	if raw == "ignore" {
		return &Directive{
			Line:   lineNum,
			Raw:    raw,
			Ignore: true,
		}
	}

	d := &Directive{
		Line: lineNum,
		Raw:  raw,
	}

	parts := strings.Split(raw, ",")
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}

		pkg := parsePackageDirective(part)
		d.Packages = append(d.Packages, pkg)
	}

	return d
}

func parsePackageDirective(s string) PackageDirective {
	colonIdx := strings.Index(s, ":")
	if colonIdx == -1 {
		return PackageDirective{Name: s, Pattern: ParsePattern("#.#.#")}
	}

	name := s[:colonIdx]
	patternStr := s[colonIdx+1:]

	if patternStr == "ignore" {
		return PackageDirective{Name: name, Ignore: true}
	}

	return PackageDirective{
		Name:    name,
		Pattern: ParsePattern(patternStr),
	}
}
