package version

import (
	"strconv"
	"strings"
)

type Version struct {
	Major  int
	Minor  int
	Patch  int
	Suffix string
	Raw    string
}

func Parse(s string) *Version {
	v := &Version{Raw: s}

	suffixIdx := strings.Index(s, "-")
	versionPart := s

	if suffixIdx != -1 {
		versionPart = s[:suffixIdx]
		v.Suffix = s[suffixIdx+1:]
	}

	parts := strings.Split(strings.TrimPrefix(versionPart, "v"), ".")
	if len(parts) >= 1 {
		v.Major, _ = strconv.Atoi(parts[0])
	}
	if len(parts) >= 2 {
		v.Minor, _ = strconv.Atoi(parts[1])
	}
	if len(parts) >= 3 {
		v.Patch, _ = strconv.Atoi(parts[2])
	}

	return v
}

func Compare(a, b *Version) int {
	if a.Major != b.Major {
		return a.Major - b.Major
	}
	if a.Minor != b.Minor {
		return a.Minor - b.Minor
	}
	if a.Patch != b.Patch {
		return a.Patch - b.Patch
	}

	return strings.Compare(a.Suffix, b.Suffix)
}

func Latest(versions []string) string {
	if len(versions) == 0 {
		return ""
	}

	parsed := make([]*Version, len(versions))
	for i, v := range versions {
		parsed[i] = Parse(v)
	}

	latest := parsed[0]
	for i := 1; i < len(parsed); i++ {
		if Compare(parsed[i], latest) > 0 {
			latest = parsed[i]
		}
	}

	return latest.Raw
}
