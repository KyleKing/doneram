package manifest

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// npmSections are the package.json objects whose specs a lock resolves.
// peerDependencies is left out: it states what a host must provide, not
// what this package installs.
var npmSections = []string{"dependencies", "devDependencies", "optionalDependencies"}

// npmFloor matches a spec whose floor can be raised in place: `^X`, `~X`,
// `>=X`, or an exact X. Anything else (`*`, `latest`, a tag, a URL,
// `workspace:`, `file:`, an `npm:` alias, a range with spaces or `||`) is
// left alone.
var npmFloor = regexp.MustCompile(`^(\^|~|>=)?(\d+(?:\.\d+){0,2}(?:-[0-9A-Za-z.-]+)?)$`)

// member is one key of a JSON object, with the byte span of its value.
type member struct {
	key        string
	start, end int
}

// npmSpec is one string spec in a dependency section, with the span of its
// content (quotes excluded).
type npmSpec struct {
	name, spec string
	start, end int
}

func npmSpecs(data []byte) ([]npmSpec, error) {
	i := skipJSONSpace(data, 0)
	if i >= len(data) || data[i] != '{' {
		return nil, errors.New("package.json is not an object")
	}
	top, _, err := jsonObject(data, i)
	if err != nil {
		return nil, err
	}
	var out []npmSpec
	for _, section := range npmSections {
		for _, m := range top {
			if m.key != section || data[m.start] != '{' {
				continue
			}
			deps, _, err := jsonObject(data, m.start)
			if err != nil {
				return nil, err
			}
			for _, d := range deps {
				if data[d.start] != '"' {
					continue
				}
				content := string(data[d.start+1 : d.end-1])
				if strings.Contains(content, `\`) {
					continue
				}
				out = append(out, npmSpec{name: d.key, spec: content, start: d.start + 1, end: d.end - 1})
			}
		}
	}
	return out, nil
}

// RaisePackageJSON rewrites the version in each raisable spec (see
// npmFloor) to the version locked holds for that package, when that is
// newer, keeping the operator. The file is edited in place, so its
// indentation and key order survive.
func RaisePackageJSON(data []byte, locked map[string]string) ([]byte, []Raise, error) {
	specs, err := npmSpecs(data)
	if err != nil {
		return nil, nil, err
	}
	var edits []edit
	var raises []Raise
	for _, s := range specs {
		m := npmFloor.FindStringSubmatch(s.spec)
		to := locked[s.name]
		if m == nil || !isVersion(to) || Compare(to, m[2]) <= 0 {
			continue
		}
		spec := m[1] + to
		edits = append(edits, edit{s.start, s.end, spec})
		raises = append(raises, Raise{Name: s.name, Old: s.spec, New: spec})
	}
	return applyEdits(data, edits), raises, nil
}

// PackageJSONConstraints lists the `^` and `~` specs, the ones a new
// major (or minor, for `~` and 0.x) can fall outside of.
func PackageJSONConstraints(data []byte) ([]Constraint, error) {
	specs, err := npmSpecs(data)
	if err != nil {
		return nil, err
	}
	var out []Constraint
	seen := map[[2]string]bool{}
	for _, s := range specs {
		k := [2]string{s.name, s.spec}
		if !npmRange.MatchString(s.spec) || seen[k] {
			continue
		}
		seen[k] = true
		out = append(out, Constraint{Name: s.name, Spec: s.spec, Ecosystem: "npm"})
	}
	return out, nil
}

// SetPackageJSONSpec replaces old with spec for name in every dependency
// section where it reads old, and returns how many changed.
func SetPackageJSONSpec(data []byte, name, old, spec string) ([]byte, int, error) {
	specs, err := npmSpecs(data)
	if err != nil {
		return nil, 0, err
	}
	var edits []edit
	for _, s := range specs {
		if s.name == name && s.spec == old {
			edits = append(edits, edit{s.start, s.end, spec})
		}
	}
	return applyEdits(data, edits), len(edits), nil
}

// jsonObject reads the object at i (which must be '{') and returns its
// members with their value spans, and the index after the object. It
// validates only what it needs to find those spans.
func jsonObject(data []byte, i int) ([]member, int, error) {
	var out []member
	i = skipJSONSpace(data, i+1)
	if i < len(data) && data[i] == '}' {
		return out, i + 1, nil
	}
	for i < len(data) {
		if data[i] != '"' {
			return nil, 0, fmt.Errorf("offset %d: expected a key", i)
		}
		end, err := jsonString(data, i)
		if err != nil {
			return nil, 0, err
		}
		var key string
		if err := json.Unmarshal(data[i:end], &key); err != nil {
			return nil, 0, fmt.Errorf("offset %d: %w", i, err)
		}
		i = skipJSONSpace(data, end)
		if i >= len(data) || data[i] != ':' {
			return nil, 0, fmt.Errorf("offset %d: expected ':'", i)
		}
		i = skipJSONSpace(data, i+1)
		vend, err := jsonValue(data, i)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, member{key: key, start: i, end: vend})
		i = skipJSONSpace(data, vend)
		if i < len(data) && data[i] == ',' {
			i = skipJSONSpace(data, i+1)
			continue
		}
		if i < len(data) && data[i] == '}' {
			return out, i + 1, nil
		}
		return nil, 0, fmt.Errorf("offset %d: expected ',' or '}'", i)
	}
	return nil, 0, errors.New("unterminated object")
}

// jsonValue returns the index just after the value at i.
func jsonValue(data []byte, i int) (int, error) {
	if i >= len(data) {
		return 0, errors.New("unexpected end of JSON")
	}
	switch data[i] {
	case '"':
		return jsonString(data, i)
	case '{':
		_, end, err := jsonObject(data, i)
		return end, err
	case '[':
		i = skipJSONSpace(data, i+1)
		if i < len(data) && data[i] == ']' {
			return i + 1, nil
		}
		for i < len(data) {
			end, err := jsonValue(data, i)
			if err != nil {
				return 0, err
			}
			i = skipJSONSpace(data, end)
			if i < len(data) && data[i] == ',' {
				i = skipJSONSpace(data, i+1)
				continue
			}
			if i < len(data) && data[i] == ']' {
				return i + 1, nil
			}
			return 0, fmt.Errorf("offset %d: expected ',' or ']'", i)
		}
		return 0, errors.New("unterminated array")
	}
	j := i
	for j < len(data) && !strings.ContainsRune(",}] \t\r\n", rune(data[j])) {
		j++
	}
	if j == i {
		return 0, fmt.Errorf("offset %d: expected a value", i)
	}
	return j, nil
}

// jsonString returns the index just after the string literal at i.
func jsonString(data []byte, i int) (int, error) {
	for j := i + 1; j < len(data); j++ {
		switch data[j] {
		case '\\':
			j++
		case '"':
			return j + 1, nil
		}
	}
	return 0, fmt.Errorf("offset %d: unterminated string", i)
}

func skipJSONSpace(data []byte, i int) int {
	for i < len(data) && (data[i] == ' ' || data[i] == '\t' || data[i] == '\r' || data[i] == '\n') {
		i++
	}
	return i
}
