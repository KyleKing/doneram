package manifest

import (
	"regexp"
	"strconv"
	"strings"
)

// Compare orders two dotted versions segment by segment, treating a
// missing segment as 0 and a segment with a tail after its number
// ("0rc1", "0-beta") as a prerelease of the bare number. That covers the
// PEP 440 and semver versions a lock writes well enough to tell which of
// two is newer; it is not a full implementation of either.
func Compare(a, b string) int {
	as := strings.Split(trimVersion(a), ".")
	bs := strings.Split(trimVersion(b), ".")
	n := max(len(as), len(bs))
	for i := 0; i < n; i++ {
		an, atail := segment(as, i)
		bn, btail := segment(bs, i)
		if an != bn {
			if an < bn {
				return -1
			}
			return 1
		}
		if atail != btail {
			switch {
			case atail == "":
				return 1
			case btail == "":
				return -1
			}
			return strings.Compare(atail, btail)
		}
	}
	return 0
}

func trimVersion(v string) string {
	v = strings.TrimPrefix(strings.TrimSpace(v), "v")
	v, _, _ = strings.Cut(v, "+")
	return v
}

func segment(parts []string, i int) (int, string) {
	if i >= len(parts) {
		return 0, ""
	}
	s := parts[i]
	j := 0
	for j < len(s) && s[j] >= '0' && s[j] <= '9' {
		j++
	}
	n, _ := strconv.Atoi(s[:j])
	return n, s[j:]
}

// isVersion reports whether v looks like a release number a floor can be
// raised to: digits first, no wildcard.
func isVersion(v string) bool {
	return v != "" && v[0] >= '0' && v[0] <= '9' && !strings.ContainsAny(v, "* ")
}

// Constraint is a declared range with an upper bound: a pyproject
// requirement capped with `<` or `<=`, or a package.json `^`/`~` spec.
type Constraint struct {
	// Name is the registry name: PEP 503 normalized for PyPI, as written
	// for npm.
	Name string
	// Spec is the range as written (">=2.31,<3", "^4.17.0").
	Spec string
	// Ecosystem is "pypi" or "npm".
	Ecosystem string
}

// Widen returns the spec that admits latest, when the constraint's upper
// bound excludes it. The floor moves to latest, as it would after a floor
// sync, and a pyproject cap moves past latest at the precision it was
// written with (<3 becomes <4, <2.5 becomes <2.8 for a 2.7.1).
func Widen(c Constraint, latest string) (string, bool) {
	if !isVersion(latest) {
		return "", false
	}
	if c.Ecosystem == "npm" {
		return widenNPM(c.Spec, latest)
	}
	return widenPyPI(c.Spec, latest)
}

var npmRange = regexp.MustCompile(`^([\^~])(\d+(?:\.\d+){0,2})$`)

// widenNPM handles `^X` and `~X`, following semver's rules for where each
// stops: ^1.2.3 <2.0.0, ^0.2.3 <0.3.0, ^0.0.3 <0.0.4, ~1.2.3 <1.3.0.
func widenNPM(spec, latest string) (string, bool) {
	m := npmRange.FindStringSubmatch(strings.TrimSpace(spec))
	if m == nil {
		return "", false
	}
	parts := strings.Split(m[2], ".")
	nums := make([]int, 3)
	for i, p := range parts {
		nums[i], _ = strconv.Atoi(p)
	}
	var upper []int
	switch {
	case m[1] == "~" && len(parts) == 1:
		upper = []int{nums[0] + 1, 0, 0}
	case m[1] == "~":
		upper = []int{nums[0], nums[1] + 1, 0}
	case nums[0] > 0 || len(parts) == 1:
		upper = []int{nums[0] + 1, 0, 0}
	case nums[1] > 0 || len(parts) == 2:
		upper = []int{0, nums[1] + 1, 0}
	default:
		upper = []int{0, 0, nums[2] + 1}
	}
	bound := strconv.Itoa(upper[0]) + "." + strconv.Itoa(upper[1]) + "." + strconv.Itoa(upper[2])
	if Compare(latest, bound) < 0 {
		return "", false
	}
	return m[1] + latest, true
}

// pyClause matches one PEP 440 version clause; the longer operators come
// first so `===` and `>=` are not read as `==` and `>`.
var pyClause = regexp.MustCompile(`(===|==|~=|!=|>=|<=|<|>)\s*([A-Za-z0-9.*+!_-]+)`)

// widenPyPI handles a spec capped with `<` or `<=`. A spec pinned with
// `==`, `~=`, or `===` is a deliberate pin, not a range, and is left
// alone.
func widenPyPI(spec, latest string) (string, bool) {
	clauses := pyClause.FindAllStringSubmatchIndex(spec, -1)
	excluded := false
	var edits []edit
	for _, c := range clauses {
		op, ver := spec[c[2]:c[3]], spec[c[4]:c[5]]
		switch op {
		case "==", "~=", "===":
			return "", false
		case "<":
			if Compare(latest, ver) >= 0 {
				excluded = true
				edits = append(edits, edit{c[4], c[5], nextCap(ver, latest)})
			}
		case "<=":
			if Compare(latest, ver) > 0 {
				excluded = true
				edits = append(edits, edit{c[4], c[5], latest})
			}
		case ">=":
			if isVersion(ver) && Compare(latest, ver) > 0 {
				edits = append(edits, edit{c[4], c[5], latest})
			}
		}
	}
	if !excluded {
		return "", false
	}
	return string(applyEdits([]byte(spec), edits)), true
}

// nextCap moves an exclusive cap just past latest, bumping the segment the
// cap was written to stop at (its last nonzero one) and keeping its
// length: <3 and latest 3.1.0 give <4, <3.0.0 gives <4.0.0, <2.5 and
// latest 2.7.1 give <2.8.
func nextCap(capVer, latest string) string {
	parts := strings.Split(trimVersion(capVer), ".")
	at := 0
	for i := range parts {
		if n, _ := segment(parts, i); n != 0 {
			at = i
		}
	}
	lat := strings.Split(trimVersion(latest), ".")
	out := make([]string, len(parts))
	for i := range parts {
		n, _ := segment(lat, i)
		switch {
		case i < at:
			out[i] = strconv.Itoa(n)
		case i == at:
			out[i] = strconv.Itoa(n + 1)
		default:
			out[i] = "0"
		}
	}
	return strings.Join(out, ".")
}
