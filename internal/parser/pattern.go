package parser

import (
	"regexp"
	"strings"
)

type VersionPattern struct {
	Major  string
	Minor  string
	Patch  string
	Suffix string
	Raw    string
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

func (p *VersionPattern) Matches(version string) bool {
	suffixIdx := strings.Index(version, "-")
	versionPart := version
	versionSuffix := ""
	if suffixIdx != -1 {
		versionPart = version[:suffixIdx]
		versionSuffix = version[suffixIdx+1:]
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

	if p.Suffix != "" && !matchSuffix(p.Suffix, versionSuffix) {
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
