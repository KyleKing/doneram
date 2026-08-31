package parser

import (
	"regexp"
	"strings"

	"github.com/kyleking/doneram/pkg/version"
)

type VersionPattern struct {
	Major  string
	Minor  string
	Patch  string
	Suffix string
	Raw    string

	// Ceiling is a hold's exclusive upper bound (e.g. "3.0.0"), never
	// crossed regardless of what the base pattern would otherwise match.
	Ceiling string
	// HoldReason is why Ceiling was set, carried through to the report.
	HoldReason string
}

func (p *VersionPattern) String() string {
	return p.Raw
}

func ParsePattern(s string) *VersionPattern {
	p := &VersionPattern{Raw: s}

	suffixIdx := strings.Index(s, "-")
	versionPart := s
	if suffixIdx != -1 {
		versionPart = s[:suffixIdx]
		p.Suffix = s[suffixIdx+1:]
	}

	parts := strings.Split(versionPart, ".")
	if len(parts) >= 1 {
		p.Major = parts[0]
	}
	if len(parts) >= 2 {
		p.Minor = parts[1]
	}
	if len(parts) >= 3 {
		p.Patch = parts[2]
	}

	return p
}

func (p *VersionPattern) Matches(v string) bool {
	suffixIdx := strings.Index(v, "-")
	versionPart := v
	versionSuffix := ""
	if suffixIdx != -1 {
		versionPart = v[:suffixIdx]
		versionSuffix = v[suffixIdx+1:]
	}

	parts := strings.Split(versionPart, ".")

	if !matchSegment(p.Major, getIndex(parts, 0)) {
		return false
	}
	if !matchSegment(p.Minor, getIndex(parts, 1)) {
		return false
	}
	if !matchSegment(p.Patch, getIndex(parts, 2)) {
		return false
	}

	if p.Suffix == "" {
		if versionSuffix != "" {
			return false
		}
	} else if !matchSuffix(p.Suffix, versionSuffix) {
		return false
	}

	if p.Ceiling != "" && version.Compare(version.Parse(v), version.Parse(p.Ceiling)) >= 0 {
		return false
	}

	return true
}

func matchSegment(pattern, value string) bool {
	if pattern == "" || pattern == "#" {
		return true
	}
	return pattern == value
}

func matchSuffix(pattern, value string) bool {
	if pattern == "*" {
		return true
	}
	if strings.HasSuffix(pattern, "*") {
		prefix := strings.TrimSuffix(pattern, "*")
		return strings.HasPrefix(value, prefix)
	}
	return pattern == value
}

func getIndex(slice []string, idx int) string {
	if idx < len(slice) {
		return slice[idx]
	}
	return ""
}

var wildcardRegex = regexp.MustCompile(`#`)

func (p *VersionPattern) ToRegex() string {
	s := regexp.QuoteMeta(p.Raw)
	s = wildcardRegex.ReplaceAllString(s, `\d+`)
	s = strings.ReplaceAll(s, `\*`, `.*`)
	return "^" + s + "$"
}
