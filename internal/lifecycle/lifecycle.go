// Package lifecycle finds the references in a repo whose support ends on a
// published date, with no config: runtimes in mise.toml and .tool-versions,
// base images, GitHub-hosted runner labels, language floors in
// pyproject.toml, package.json, and go.mod, and LLM model IDs in code. Each
// is mapped to an https://endoflife.date product and the cycle it belongs
// to; the lookup itself is left to the caller.
package lifecycle

import (
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/kyleking/doneram/internal/digest"
)

// Source names where a reference was found.
type Source string

const (
	Tool       Source = "tool"
	Image      Source = "image"
	Runner     Source = "runner"
	Constraint Source = "constraint"
	Model      Source = "model"
)

// Ref is one reference with a lifecycle.
type Ref struct {
	Source  Source
	File    string
	Line    int
	Name    string
	Product string
	// Version is matched against the product's cycles: by version number
	// (3.13.5 in 3.13) unless ByID, then by identifier (a model ID or a
	// runner label, equal to a cycle or extending it at a dash).
	Version string
	ByID    bool
}

// toolProducts maps mise tool names to endoflife.date products.
var toolProducts = map[string]string{
	"python": "python", "node": "nodejs", "nodejs": "nodejs", "go": "go", "golang": "go",
	"ruby": "ruby", "java": "eclipse-temurin", "deno": "deno", "bun": "bun",
	"terraform": "terraform", "kubectl": "kubernetes", "postgres": "postgresql",
	"redis": "redis", "php": "php", "elixir": "elixir", "erlang": "erlang",
	"dotnet": "dotnet", "kotlin": "kotlin", "rust": "rust", "scala": "scala",
	"perl": "perl", "pnpm": "pnpm", "yarn": "yarn",
}

// imageProducts maps an image repository (Docker Hub's library images go
// by their short name) to an endoflife.date product.
var imageProducts = map[string]string{
	"python": "python", "node": "nodejs", "golang": "go", "ruby": "ruby",
	"postgres": "postgresql", "redis": "redis", "mysql": "mysql", "mariadb": "mariadb",
	"alpine": "alpine-linux", "debian": "debian", "ubuntu": "ubuntu",
	"eclipse-temurin": "eclipse-temurin", "amazoncorretto": "amazon-corretto",
	"php": "php", "nginx": "nginx", "mongo": "mongodb", "rabbitmq": "rabbitmq",
	"elasticsearch": "elasticsearch", "amazonlinux": "amazon-linux", "valkey/valkey": "valkey",
	"memcached": "memcached", "haproxy": "haproxy", "caddy": "caddy", "traefik": "traefik",
	"rockylinux": "rocky-linux", "almalinux": "almalinux", "fedora": "fedora",
	"elixir": "elixir", "erlang": "erlang", "rust": "rust", "perl": "perl",
	"mcr.microsoft.com/dotnet/sdk": "dotnet", "mcr.microsoft.com/dotnet/aspnet": "dotnet",
	"mcr.microsoft.com/dotnet/runtime": "dotnet",
}

// imageCycle is the leading version of a tag ("3.13-slim" → "3.13",
// "22-alpine3.20" → "22"), or a bare word tag that may be a codename
// ("bookworm").
var imageCycle = regexp.MustCompile(`^v?(\d+(?:\.\d+)*)|^([a-z]+)$`)

var skipDirs = map[string]bool{
	".git": true, "node_modules": true, "vendor": true, ".venv": true, "venv": true,
	"target": true, "dist": true, "build": true, "testdata": true, "fixtures": true,
	"tests": true, "__tests__": true,
}

// Scan returns every lifecycle reference under baseDir, sorted by file
// and line.
func Scan(baseDir string) ([]Ref, error) {
	var out []Ref
	err := filepath.WalkDir(baseDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		name := d.Name()
		if d.IsDir() {
			if path != baseDir && (skipDirs[name] || (strings.HasPrefix(name, ".") && name != ".github")) {
				return filepath.SkipDir
			}
			return nil
		}
		var refs []Ref
		var err2 error
		lower := strings.ToLower(name)
		rel, _ := filepath.Rel(baseDir, path)
		rel = filepath.ToSlash(rel)
		switch {
		case name == "mise.toml" || name == ".mise.toml" || name == ".tool-versions":
			refs, err2 = scanTools(path)
		case lower == "dockerfile" || strings.HasPrefix(lower, "dockerfile.") || strings.HasSuffix(lower, ".dockerfile") ||
			((strings.HasPrefix(lower, "compose") || strings.HasPrefix(lower, "docker-compose")) && isYAML(lower)):
			refs, err2 = scanImages(path)
		case strings.HasPrefix(rel, ".github/workflows/") && isYAML(lower):
			refs, err2 = scanRunners(path)
		case name == "pyproject.toml" || name == "package.json" || name == "go.mod":
			refs, err2 = scanConstraint(path)
		}
		if err2 != nil {
			return err2
		}
		out = append(out, refs...)
		if modelFile(lower) {
			models, err := scanModels(path)
			if err != nil {
				return err
			}
			out = append(out, models...)
		}
		return nil
	})
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].File != out[j].File {
			return out[i].File < out[j].File
		}
		return out[i].Line < out[j].Line
	})
	return out, err
}

func isYAML(name string) bool {
	return strings.HasSuffix(name, ".yml") || strings.HasSuffix(name, ".yaml")
}

func lines(path string) ([]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return strings.Split(string(data), "\n"), nil
}

var miseTool = regexp.MustCompile(`^\s*"?([A-Za-z0-9_.-]+)"?\s*=\s*"v?(\d[\w.]*)"`)

func scanTools(path string) ([]Ref, error) {
	ls, err := lines(path)
	if err != nil {
		return nil, err
	}
	toml := filepath.Base(path) != ".tool-versions"
	section := ""
	var out []Ref
	for i, line := range ls {
		var tool, ver string
		if toml {
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, "[") {
				section = strings.Trim(trimmed, "[] ")
				continue
			}
			m := miseTool.FindStringSubmatch(line)
			if section != "tools" || m == nil {
				continue
			}
			tool, ver = m[1], m[2]
		} else {
			f := strings.Fields(line)
			if len(f) < 2 || strings.HasPrefix(f[0], "#") {
				continue
			}
			tool, ver = f[0], strings.TrimPrefix(f[1], "v")
		}
		product, ok := toolProducts[tool]
		if !ok || ver == "" || ver[0] < '0' || ver[0] > '9' {
			continue
		}
		out = append(out, Ref{Source: Tool, File: path, Line: i + 1, Name: tool, Product: product, Version: ver})
	}
	return out, nil
}

var (
	fromLine    = regexp.MustCompile(`(?i)^\s*FROM\s+(?:--\S+\s+)*(\S+)`)
	composeLine = regexp.MustCompile(`^\s*image:\s*['"]?([^'"\s#]+)`)
)

func scanImages(path string) ([]Ref, error) {
	ls, err := lines(path)
	if err != nil {
		return nil, err
	}
	var out []Ref
	for i, line := range ls {
		m := fromLine.FindStringSubmatch(line)
		if m == nil {
			m = composeLine.FindStringSubmatch(line)
		}
		if m == nil || strings.Contains(m[1], "$") {
			continue
		}
		ref, err := digest.Parse(m[1])
		if err != nil || !ref.ExplicitTag() {
			continue
		}
		repo := strings.TrimPrefix(ref.Repository, "library/")
		if ref.Registry != "registry-1.docker.io" {
			repo = ref.Registry + "/" + ref.Repository
		}
		product, ok := imageProducts[repo]
		if !ok {
			continue
		}
		c := imageCycle.FindStringSubmatch(ref.Tag)
		if c == nil {
			continue
		}
		ver := c[1] + c[2]
		if ver == "latest" || ver == "lts" || ver == "stable" {
			continue
		}
		out = append(out, Ref{Source: Image, File: path, Line: i + 1, Name: ref.Name + ":" + ref.Tag, Product: product, Version: ver})
	}
	return out, nil
}

// runnerLabel is a GitHub-hosted runner image label pinned to a release.
// `-latest` labels move with GitHub and never reach end of life.
// bareListItem is a YAML list entry holding nothing but a runner label, as
// in a matrix written one OS per line.
var bareListItem = regexp.MustCompile(`^\s*-\s*['"]?(?:ubuntu|macos|windows)-[\w.-]+['"]?\s*$`)

var runnerLabel = regexp.MustCompile(`\b((?:ubuntu|macos|windows)-(?:\d[\w.-]*|\d+))\b`)

func scanRunners(path string) ([]Ref, error) {
	ls, err := lines(path)
	if err != nil {
		return nil, err
	}
	var out []Ref
	for i, line := range ls {
		code, _, _ := strings.Cut(line, "#")
		if !strings.Contains(code, "runs-on") && !strings.Contains(code, "os:") && !bareListItem.MatchString(code) {
			continue
		}
		for _, m := range runnerLabel.FindAllStringSubmatch(code, -1) {
			out = append(out, Ref{Source: Runner, File: path, Line: i + 1, Name: m[1], Product: "github-actions-runner-images", Version: m[1], ByID: true})
		}
	}
	return out, nil
}

var (
	requiresPython = regexp.MustCompile(`^\s*requires-python\s*=\s*"[^"\d]*(\d+\.\d+)`)
	goDirective    = regexp.MustCompile(`^go\s+(\d+\.\d+)`)
	nodeFloor      = regexp.MustCompile(`(\d+)`)
)

// scanConstraint reads the oldest language version a project still claims
// to support: the floor of requires-python, engines.node, or go.mod's go
// directive. Supporting a runtime past its end of life is worth knowing
// even when every pin is current.
func scanConstraint(path string) ([]Ref, error) {
	ls, err := lines(path)
	if err != nil {
		return nil, err
	}
	switch filepath.Base(path) {
	case "pyproject.toml":
		for i, line := range ls {
			if m := requiresPython.FindStringSubmatch(line); m != nil {
				return []Ref{{Source: Constraint, File: path, Line: i + 1, Name: "requires-python", Product: "python", Version: m[1]}}, nil
			}
		}
	case "go.mod":
		for i, line := range ls {
			if m := goDirective.FindStringSubmatch(line); m != nil {
				return []Ref{{Source: Constraint, File: path, Line: i + 1, Name: "go directive", Product: "go", Version: m[1]}}, nil
			}
		}
	case "package.json":
		var pkg struct {
			Engines struct {
				Node string `json:"node"`
			} `json:"engines"`
		}
		if json.Unmarshal([]byte(strings.Join(ls, "\n")), &pkg) != nil || pkg.Engines.Node == "" {
			return nil, nil
		}
		m := nodeFloor.FindString(pkg.Engines.Node)
		if m == "" {
			return nil, nil
		}
		line := 1
		for i, l := range ls {
			if strings.Contains(l, `"node"`) && strings.Contains(l, pkg.Engines.Node) {
				line = i + 1
			}
		}
		return []Ref{{Source: Constraint, File: path, Line: line, Name: "engines.node", Product: "nodejs", Version: m}}, nil
	}
	return nil, nil
}

// modelExts are files that may name a hosted model in a string.
var modelExts = []string{
	".py", ".ts", ".tsx", ".js", ".jsx", ".mjs", ".go", ".rb", ".java", ".kt", ".rs",
	".cs", ".php", ".toml", ".yaml", ".yml", ".json", ".env", ".pkl", ".sh",
}

// testFile matches test sources, which name retired models on purpose.
var testFile = regexp.MustCompile(`(_test\.\w+|\.(test|spec)\.\w+|^test_.*\.py|^conftest\.py)$`)

func modelFile(name string) bool {
	if strings.HasSuffix(name, "lock.json") || strings.HasSuffix(name, ".lock") || testFile.MatchString(name) {
		return false
	}
	for _, ext := range modelExts {
		if strings.HasSuffix(name, ext) {
			return true
		}
	}
	return false
}

// modelID matches a quoted model identifier. Quoting keeps prose and URLs
// out; an ID no cycle covers is dropped by the caller, so an unrelated
// string like "gpt-like" costs nothing.
var modelID = regexp.MustCompile(`["'` + "`" + `]((claude-[a-z0-9.-]+)|(gpt-[a-z0-9.-]+)|(o\d(?:-[a-z0-9-]+)?)|((?:text-embedding|dall-e|whisper|tts)-[a-z0-9.-]+))["'` + "`" + `]`)

func scanModels(path string) ([]Ref, error) {
	info, err := os.Stat(path)
	if err != nil || info.Size() > 1<<20 {
		return nil, err
	}
	ls, err := lines(path)
	if err != nil {
		return nil, err
	}
	var out []Ref
	seen := map[string]bool{}
	for i, line := range ls {
		for _, m := range modelID.FindAllStringSubmatch(line, -1) {
			id := m[1]
			if seen[id] {
				continue
			}
			seen[id] = true
			product := "openai-api-models"
			if strings.HasPrefix(id, "claude-") {
				product = "claude"
			}
			out = append(out, Ref{Source: Model, File: path, Line: i + 1, Name: id, Product: product, Version: id, ByID: true})
		}
	}
	return out, nil
}
