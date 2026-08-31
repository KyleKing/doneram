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
	if hold, ok := parseHoldPattern(s); ok {
		return PackageDirective{Pattern: hold}
	}

	colonIdx := strings.Index(s, ":")
	if colonIdx == -1 {
		return PackageDirective{Name: s, Pattern: ParsePattern("#.#.#")}
	}

	name := s[:colonIdx]
	patternStr := s[colonIdx+1:]

	if patternStr == "ignore" {
		return PackageDirective{Name: name, Ignore: true}
	}

	if hold, ok := parseHoldPattern(patternStr); ok {
		return PackageDirective{Name: name, Pattern: hold}
	}

	return PackageDirective{
		Name:    name,
		Pattern: ParsePattern(patternStr),
	}
}

// holdRegex matches `hold[reason; <ceiling]`, e.g.
// `hold[cgo build breaks on 3.0; <3.0.0]`.
var holdRegex = regexp.MustCompile(`^hold\[([^;]+);\s*<(.+)\]$`)

// parseHoldPattern recognizes a hold directive and returns an unconstrained
// pattern narrowed by the hold's ceiling, so it keeps taking updates below
// the ceiling rather than freezing the pin outright.
func parseHoldPattern(s string) (*VersionPattern, bool) {
	matches := holdRegex.FindStringSubmatch(s)
	if matches == nil {
		return nil, false
	}

	p := ParsePattern("#.#.#")
	p.Raw = s
	p.HoldReason = strings.TrimSpace(matches[1])
	p.Ceiling = strings.TrimSpace(matches[2])
	return p, true
}
