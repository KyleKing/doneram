package manifest

import (
	"bufio"
	"bytes"
	"encoding/json"
	"strings"

	"gopkg.in/yaml.v3"
)

// LockedUV reads uv.lock's `[[package]]` name and version pairs, keyed by
// PEP 503 name. A package locked at several versions (uv forks the
// resolution per Python version or platform) maps to the lowest, the only
// floor every fork satisfies.
func LockedUV(data []byte) map[string]string {
	out := map[string]string{}
	var name, ver string
	inPackage := false
	flush := func() {
		if inPackage && name != "" && isVersion(ver) {
			k := NormalizePyPI(name)
			if cur, ok := out[k]; !ok || Compare(ver, cur) < 0 {
				out[k] = ver
			}
		}
		name, ver = "", ""
	}
	scanner := bufio.NewScanner(bytes.NewReader(data))
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, "[") {
			flush()
			inPackage = line == "[[package]]"
			continue
		}
		if !inPackage {
			continue
		}
		key, val, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		val = strings.Trim(strings.TrimSpace(val), `"`)
		switch strings.TrimSpace(key) {
		case "name":
			name = val
		case "version":
			ver = val
		}
	}
	flush()
	return out
}

// LockedNPM reads package-lock.json (v2 or v3) for the version installed
// at the top of node_modules for each package, which is the copy a
// package.json spec resolves to.
func LockedNPM(data []byte) (map[string]string, error) {
	var lock struct {
		Packages map[string]struct {
			Version string `json:"version"`
		} `json:"packages"`
	}
	if err := json.Unmarshal(data, &lock); err != nil {
		return nil, err
	}
	out := map[string]string{}
	for key, p := range lock.Packages {
		name, ok := strings.CutPrefix(key, "node_modules/")
		if !ok || strings.Contains(name, "/node_modules/") {
			continue
		}
		out[name] = p.Version
	}
	return out, nil
}

// pnpmDeps is one importer's dependency sections in pnpm-lock.yaml.
type pnpmDeps struct {
	Dependencies         map[string]pnpmDep `yaml:"dependencies"`
	DevDependencies      map[string]pnpmDep `yaml:"devDependencies"`
	OptionalDependencies map[string]pnpmDep `yaml:"optionalDependencies"`
}

type pnpmDep struct {
	Version string `yaml:"version"`
}

// UnmarshalYAML also takes the bare version string a v5 lock writes in
// place of v6's {specifier, version} map.
func (d *pnpmDep) UnmarshalYAML(n *yaml.Node) error {
	if n.Kind == yaml.ScalarNode {
		d.Version = n.Value
		return nil
	}
	var m struct {
		Version string `yaml:"version"`
	}
	if err := n.Decode(&m); err != nil {
		return err
	}
	d.Version = m.Version
	return nil
}

// LockedPNPM reads the root importer of pnpm-lock.yaml (v9's
// `importers['.']`, or the top-level sections of an older single-project
// lock), with any peer suffix ("18.2.0(react@18.2.0)") dropped.
func LockedPNPM(data []byte) (map[string]string, error) {
	var lock struct {
		Importers map[string]pnpmDeps `yaml:"importers"`
		pnpmDeps  `yaml:",inline"`
	}
	if err := yaml.Unmarshal(data, &lock); err != nil {
		return nil, err
	}
	root, ok := lock.Importers["."]
	if !ok {
		root = lock.pnpmDeps
	}
	out := map[string]string{}
	for _, section := range []map[string]pnpmDep{root.Dependencies, root.DevDependencies, root.OptionalDependencies} {
		for name, d := range section {
			v, _, _ := strings.Cut(d.Version, "(")
			out[name] = v
		}
	}
	return out, nil
}
