package manifest

import (
	"regexp"
	"strings"
)

// NormalizePyPI is a distribution name in PEP 503 form: lowercase, with
// every run of '-', '_', and '.' collapsed to one '-'. uv.lock writes names
// this way, and a pyproject may not.
func NormalizePyPI(name string) string {
	return strings.ToLower(pySeparators.ReplaceAllString(name, "-"))
}

var pySeparators = regexp.MustCompile(`[-_.]+`)

// requirement is one PEP 508 string found in a dependency array, with the
// byte span its version spec occupies in the file.
type requirement struct {
	name       string
	spec       string
	start, end int
}

var pyName = regexp.MustCompile(`^\s*([A-Za-z0-9][A-Za-z0-9._-]*)\s*(\[[^\]]*\])?\s*`)

// parseRequirement splits a requirement string's spec from its name,
// extras, and markers. offset is where the string's content starts in the
// file. A direct reference (`name @ url`) has no spec to raise.
func parseRequirement(s string, offset int) (requirement, bool) {
	m := pyName.FindStringSubmatchIndex(s)
	if m == nil {
		return requirement{}, false
	}
	rest := s[m[1]:]
	if strings.HasPrefix(rest, "@") {
		return requirement{}, false
	}
	spec := rest
	if i := strings.Index(spec, ";"); i >= 0 {
		spec = spec[:i]
	}
	spec = strings.TrimRight(spec, " \t")
	return requirement{
		name:  s[m[2]:m[3]],
		spec:  spec,
		start: offset + m[1],
		end:   offset + m[1] + len(spec),
	}, true
}

// pyRequirements finds every requirement string in the arrays a pyproject
// declares dependencies in: `[project] dependencies`, each array under
// `[project.optional-dependencies]`, and each under `[dependency-groups]`.
// It reads just enough TOML to find those arrays and their strings, so the
// file can be edited in place.
func pyRequirements(data []byte) []requirement {
	var out []requirement
	table := ""
	i := 0
	for i < len(data) {
		i = skipSpace(data, i, true)
		if i >= len(data) {
			break
		}
		switch data[i] {
		case '#':
			i = lineEnd(data, i)
			continue
		case '[':
			end := lineEnd(data, i)
			header := strings.TrimSpace(stripComment(string(data[i:end])))
			header = strings.Trim(header, "[]")
			table = normalizeKey(header)
			i = end
			continue
		}
		// key = value
		eq := i
		for eq < len(data) && data[eq] != '=' && data[eq] != '\n' {
			eq++
		}
		if eq >= len(data) || data[eq] != '=' {
			i = lineEnd(data, i)
			continue
		}
		key := normalizeKey(string(data[i:eq]))
		v := skipSpace(data, eq+1, false)
		strs, end := scanValue(data, v)
		if dependencyArray(table, key) && v < len(data) && data[v] == '[' {
			for _, s := range strs {
				if r, ok := parseRequirement(string(data[s.start:s.end]), s.start); ok {
					out = append(out, r)
				}
			}
		}
		i = end
	}
	return out
}

func dependencyArray(table, key string) bool {
	switch table {
	case "project":
		return key == "dependencies" || strings.HasPrefix(key, "optional-dependencies.")
	case "project.optional-dependencies", "dependency-groups":
		return !strings.Contains(key, ".")
	case "":
		return key == "project.dependencies" || strings.HasPrefix(key, "project.optional-dependencies.") || strings.HasPrefix(key, "dependency-groups.")
	}
	return false
}

// normalizeKey turns a TOML key or table header as written (`"project" .
// dependencies`) into its dotted form.
func normalizeKey(k string) string {
	parts := strings.Split(k, ".")
	for i, p := range parts {
		parts[i] = strings.Trim(strings.TrimSpace(p), `"'`)
	}
	return strings.Join(parts, ".")
}

func stripComment(s string) string {
	if i := strings.Index(s, "#"); i >= 0 {
		return s[:i]
	}
	return s
}

func lineEnd(data []byte, i int) int {
	for i < len(data) && data[i] != '\n' {
		i++
	}
	return i
}

func skipSpace(data []byte, i int, newlines bool) int {
	for i < len(data) && (data[i] == ' ' || data[i] == '\t' || data[i] == '\r' || (newlines && data[i] == '\n')) {
		i++
	}
	return i
}

// span is the content of a string literal, quotes excluded.
type span struct{ start, end int }

// scanValue skips one TOML value starting at i and returns where it ends.
// It also returns the single-line strings that are direct elements of an
// array value (not inside a nested array or inline table), which is where
// requirement strings live; one with an escape is left out, since editing
// it in place could break the escape.
func scanValue(data []byte, i int) ([]span, int) {
	var strs []span
	depth, braces := 0, 0
	for i < len(data) {
		c := data[i]
		switch {
		case c == '#':
			i = lineEnd(data, i)
			if depth == 0 && braces == 0 {
				return strs, i
			}
			continue
		case c == '"' || c == '\'':
			s, end, ok := scanString(data, i)
			if ok && depth == 1 && braces == 0 {
				strs = append(strs, s)
			}
			i = end
			if depth == 0 && braces == 0 {
				return strs, lineEnd(data, i)
			}
			continue
		case c == '[':
			depth++
		case c == ']':
			depth--
			if depth == 0 && braces == 0 {
				return strs, lineEnd(data, i+1)
			}
		case c == '{':
			braces++
		case c == '}':
			braces--
			if depth == 0 && braces == 0 {
				return strs, lineEnd(data, i+1)
			}
		case c == '\n' && depth == 0 && braces == 0:
			return strs, i
		}
		i++
	}
	return strs, i
}

// scanString reads the string literal at i and returns its content span,
// the index after it, and whether the content can be edited in place: a
// single-line string with no escapes.
func scanString(data []byte, i int) (span, int, bool) {
	q := data[i]
	triple := string([]byte{q, q, q})
	if strings.HasPrefix(string(data[i:min(i+3, len(data))]), triple) {
		end := strings.Index(string(data[i+3:]), triple)
		if end < 0 {
			return span{}, len(data), false
		}
		return span{}, i + 3 + end + 3, false
	}
	editable := true
	j := i + 1
	for j < len(data) && data[j] != q && data[j] != '\n' {
		if q == '"' && data[j] == '\\' {
			editable = false
			j++
		}
		j++
	}
	if j >= len(data) || data[j] != q {
		return span{}, j, false
	}
	return span{i + 1, j}, j + 1, editable
}

// RaisePyProject raises every `>=X` floor in a pyproject's dependency
// arrays to the version locked holds for that package (keyed by
// NormalizePyPI name), when that is newer. Extras, markers, and every
// other clause stay as written, and a floor is never lowered.
func RaisePyProject(data []byte, locked map[string]string) ([]byte, []Raise) {
	var edits []edit
	var raises []Raise
	for _, r := range pyRequirements(data) {
		to := locked[NormalizePyPI(r.name)]
		if !isVersion(to) {
			continue
		}
		spec, ok := raisePyFloor(r.spec, to)
		if !ok {
			continue
		}
		edits = append(edits, edit{r.start, r.end, spec})
		raises = append(raises, Raise{Name: r.name, Old: r.spec, New: spec})
	}
	return applyEdits(data, edits), raises
}

func raisePyFloor(spec, to string) (string, bool) {
	var edits []edit
	for _, c := range pyClause.FindAllStringSubmatchIndex(spec, -1) {
		if spec[c[2]:c[3]] != ">=" {
			continue
		}
		if ver := spec[c[4]:c[5]]; isVersion(ver) && Compare(to, ver) > 0 {
			edits = append(edits, edit{c[4], c[5], to})
		}
	}
	if len(edits) == 0 {
		return "", false
	}
	return string(applyEdits([]byte(spec), edits)), true
}

// PyProjectConstraints lists the requirements capped from above, the ones
// a new major can fall outside of. Names come back PEP 503 normalized.
func PyProjectConstraints(data []byte) []Constraint {
	var out []Constraint
	seen := map[[2]string]bool{}
	for _, r := range pyRequirements(data) {
		capped := false
		for _, c := range pyClause.FindAllStringSubmatch(r.spec, -1) {
			if c[1] == "<" || c[1] == "<=" {
				capped = true
			}
		}
		k := [2]string{NormalizePyPI(r.name), r.spec}
		if !capped || seen[k] {
			continue
		}
		seen[k] = true
		out = append(out, Constraint{Name: k[0], Spec: r.spec, Ecosystem: "pypi"})
	}
	return out
}

// SetPyProjectSpec replaces old with spec in every requirement for name
// (compared PEP 503 normalized) whose spec reads old, and returns how many
// changed.
func SetPyProjectSpec(data []byte, name, old, spec string) ([]byte, int) {
	var edits []edit
	for _, r := range pyRequirements(data) {
		if NormalizePyPI(r.name) == NormalizePyPI(name) && r.spec == old {
			edits = append(edits, edit{r.start, r.end, spec})
		}
	}
	return applyEdits(data, edits), len(edits)
}
