// Package lockfile reads the resolved dependency graph a package manager
// writes down (uv.lock, Cargo.lock, package-lock.json, go.mod, ...) so every
// transitive dependency can be checked against advisories, not just the
// literals doneram pins itself.
//
// Reading is all it does. Raising a version is handed back to the package
// manager that owns the lockfile through FixCommand, since re-resolving a
// graph is exactly the job doneram leaves to uv, cargo, npm, and go.
package lockfile

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// Package is one resolved dependency in a lockfile.
type Package struct {
	Name    string
	Version string
	// Ecosystem is the OSV ecosystem name.
	Ecosystem string
	// File is the lockfile the package was read from.
	File string
}

// parser reads one lockfile format.
type parser struct {
	ecosystem string
	parse     func(path string) ([]Package, error)
}

// parsers is keyed by file name.
var parsers = map[string]parser{
	"uv.lock":           {"PyPI", parseTOMLPackages},
	"poetry.lock":       {"PyPI", parseTOMLPackages},
	"pdm.lock":          {"PyPI", parseTOMLPackages},
	"Cargo.lock":        {"crates.io", parseTOMLPackages},
	"package-lock.json": {"npm", parseNPM},
	"pnpm-lock.yaml":    {"npm", parsePNPM},
	"go.mod":            {"Go", parseGoMod},
	"Gemfile.lock":      {"RubyGems", parseGemfile},
	"composer.lock":     {"Packagist", parseComposer},
}

// skipDirs are never descended into: vendored or installed copies of a
// graph the lockfile beside them already describes.
var skipDirs = map[string]bool{
	".git": true, "node_modules": true, "vendor": true, ".venv": true,
	"venv": true, "target": true, ".tox": true, "dist": true, "build": true,
}

// Find returns every lockfile under root, sorted.
func Find(root string) ([]string, error) {
	var out []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if path != root && (skipDirs[d.Name()] || strings.HasPrefix(d.Name(), ".")) && d.Name() != ".github" {
				return filepath.SkipDir
			}
			return nil
		}
		if _, ok := parsers[d.Name()]; ok {
			out = append(out, path)
		}
		return nil
	})
	sort.Strings(out)
	return out, err
}

// Parse reads the packages a lockfile resolves, deduplicated by name and
// version.
func Parse(path string) ([]Package, error) {
	p, ok := parsers[filepath.Base(path)]
	if !ok {
		return nil, fmt.Errorf("%s: not a known lockfile", path)
	}
	pkgs, err := p.parse(path)
	if err != nil {
		return nil, fmt.Errorf("parsing %s: %w", path, err)
	}
	seen := map[[2]string]bool{}
	out := pkgs[:0]
	for _, pkg := range pkgs {
		if pkg.Name == "" || pkg.Version == "" {
			continue
		}
		k := [2]string{pkg.Name, pkg.Version}
		if seen[k] {
			continue
		}
		seen[k] = true
		pkg.Ecosystem = p.ecosystem
		pkg.File = path
		out = append(out, pkg)
	}
	return out, nil
}

// parseTOMLPackages reads the `[[package]]` tables uv, poetry, pdm, and
// cargo all write, each with a `name` and `version` key. Only those two
// keys are needed, so a line scan stands in for a TOML parser.
func parseTOMLPackages(path string) ([]Package, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()

	var out []Package
	var cur *Package
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, "[") {
			if line == "[[package]]" {
				out = append(out, Package{})
				cur = &out[len(out)-1]
			} else {
				cur = nil
			}
			continue
		}
		if cur == nil {
			continue
		}
		key, val, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		val = strings.Trim(strings.TrimSpace(val), `"`)
		switch strings.TrimSpace(key) {
		case "name":
			cur.Name = val
		case "version":
			cur.Version = val
		case "source":
			// A git or path source has no registry version to advise on.
			if strings.HasPrefix(val, "git+") || strings.Contains(val, "editable") || strings.Contains(val, "path") {
				cur.Version = ""
			}
		}
	}
	return out, scanner.Err()
}

// parseNPM reads package-lock.json v2/v3 `packages`, keyed by install path,
// falling back to v1's nested `dependencies`.
func parseNPM(path string) ([]Package, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var lock struct {
		Packages map[string]struct {
			Name    string `json:"name"`
			Version string `json:"version"`
			Link    bool   `json:"link"`
		} `json:"packages"`
		Dependencies map[string]npmV1Dep `json:"dependencies"`
	}
	if err := json.Unmarshal(data, &lock); err != nil {
		return nil, err
	}
	var out []Package
	for key, p := range lock.Packages {
		if key == "" || p.Link {
			continue
		}
		name := p.Name
		if name == "" {
			i := strings.LastIndex(key, "node_modules/")
			if i < 0 {
				continue
			}
			name = key[i+len("node_modules/"):]
		}
		out = append(out, Package{Name: name, Version: p.Version})
	}
	if len(lock.Packages) == 0 {
		walkNPMV1(lock.Dependencies, &out)
	}
	return out, nil
}

type npmV1Dep struct {
	Version      string              `json:"version"`
	Dependencies map[string]npmV1Dep `json:"dependencies"`
}

func walkNPMV1(deps map[string]npmV1Dep, out *[]Package) {
	for name, d := range deps {
		*out = append(*out, Package{Name: name, Version: d.Version})
		walkNPMV1(d.Dependencies, out)
	}
}

// parsePNPM reads pnpm-lock.yaml's `packages` keys: `/name@1.2.3` (v6) or
// `name@1.2.3` (v9), with any peer suffix in parentheses dropped.
func parsePNPM(path string) ([]Package, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var lock struct {
		Packages map[string]any `yaml:"packages"`
	}
	if err := yaml.Unmarshal(data, &lock); err != nil {
		return nil, err
	}
	var out []Package
	for key := range lock.Packages {
		key = strings.TrimPrefix(key, "/")
		key, _, _ = strings.Cut(key, "(")
		at := strings.LastIndex(key, "@")
		if at <= 0 {
			continue
		}
		out = append(out, Package{Name: key[:at], Version: key[at+1:]})
	}
	return out, nil
}

// parseGoMod reads go.mod's require directives, which since Go 1.17 list
// every module the build needs, indirect ones included. Replaced and
// pseudo-version modules are still reported: OSV understands both.
func parseGoMod(path string) ([]Package, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var out []Package
	inBlock := false
	for _, line := range strings.Split(string(data), "\n") {
		line, _, _ = strings.Cut(line, "//")
		fields := strings.Fields(line)
		switch {
		case len(fields) == 0:
			continue
		case inBlock && fields[0] == ")":
			inBlock = false
			continue
		case fields[0] == "require" && len(fields) >= 2 && fields[1] == "(":
			inBlock = true
			continue
		case fields[0] == "require" && len(fields) >= 3:
			fields = fields[1:]
		case !inBlock:
			continue
		}
		if len(fields) >= 2 {
			out = append(out, Package{Name: fields[0], Version: fields[1]})
		}
	}
	return out, nil
}

// parseGemfile reads the `specs:` entries of Gemfile.lock's GEM section:
// four-space indented `name (version)` lines.
func parseGemfile(path string) ([]Package, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var out []Package
	section := ""
	for _, line := range strings.Split(string(data), "\n") {
		if line != "" && line[0] != ' ' {
			section = strings.TrimSpace(line)
			continue
		}
		if section != "GEM" || !strings.HasPrefix(line, "    ") || strings.HasPrefix(line, "     ") {
			continue
		}
		name, rest, ok := strings.Cut(strings.TrimSpace(line), " (")
		if !ok {
			continue
		}
		ver := strings.TrimSuffix(rest, ")")
		// A platform-specific gem is "1.2.3-x86_64-linux".
		ver, _, _ = strings.Cut(ver, "-")
		out = append(out, Package{Name: name, Version: ver})
	}
	return out, nil
}

func parseComposer(path string) ([]Package, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var lock struct {
		Packages    []composerPkg `json:"packages"`
		PackagesDev []composerPkg `json:"packages-dev"`
	}
	if err := json.Unmarshal(data, &lock); err != nil {
		return nil, err
	}
	var out []Package
	for _, p := range append(lock.Packages, lock.PackagesDev...) {
		out = append(out, Package{Name: p.Name, Version: strings.TrimPrefix(p.Version, "v")})
	}
	return out, nil
}

type composerPkg struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// FixCommand is the package manager invocation that moves one package to
// version inside the lockfile at path, run from the lockfile's directory.
// It raises a transitive dependency without touching the declared
// constraints where the tool allows it.
func FixCommand(path, name, version string) []string {
	switch filepath.Base(path) {
	case "uv.lock":
		return []string{"uv", "lock", "--upgrade-package", name + "==" + version}
	case "poetry.lock":
		return []string{"poetry", "update", "--lock", name}
	case "pdm.lock":
		return []string{"pdm", "update", "--update-eager", "--no-sync", name}
	case "Cargo.lock":
		return []string{"cargo", "update", "--package", name, "--precise", version}
	case "package-lock.json":
		return []string{"npm", "update", "--package-lock-only", name}
	case "pnpm-lock.yaml":
		return []string{"pnpm", "update", "--lockfile-only", name}
	case "go.mod":
		if !strings.HasPrefix(version, "v") {
			version = "v" + version
		}
		return []string{"go", "get", name + "@" + version}
	case "Gemfile.lock":
		return []string{"bundle", "update", "--conservative", name}
	case "composer.lock":
		return []string{"composer", "update", "--no-install", "--with-dependencies", name}
	}
	return nil
}
