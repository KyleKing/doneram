package discover

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/kyleking/doneram/internal/engine"
	"github.com/kyleking/doneram/internal/locator"
	"github.com/kyleking/doneram/internal/parser"
)

// Terraform pins provider and module versions in .tf files. A provider's
// version sits in a required_providers entry beside its source, a module's
// beside its registry source, and several share a file, so each pin is
// located by the exact lines that tie its version to its source.
//
// Two forms are pins: an exact version ("5.1.2" or "= 5.1.2"), which
// updates to the newest release, and a pessimistic constraint
// ("~> 5.0"), which stays inside the range it names while its floor rises
// with the precision it was written at, so `~> 5.0` becomes `~> 5.51`.
// Open ranges (">= 4.0") are left alone.

var (
	tfSource  = regexp.MustCompile(`\bsource\s*=\s*"([^"]+)"`)
	tfVersion = regexp.MustCompile(`\bversion\s*=\s*"(=\s*|~>\s*)?(\d+(?:\.\d+){0,2})"`)
	tfModule  = regexp.MustCompile(`^\s*module\s+"[^"]+"\s*\{`)
	tfReqProv = regexp.MustCompile(`^\s*required_providers\s*\{`)
	tfEntry   = regexp.MustCompile(`^\s*([A-Za-z0-9_-]+)\s*=\s*\{`)
)

// registryModule is namespace/name/provider on the public registry, as
// opposed to a local path, git URL, or another registry host.
var registryModule = regexp.MustCompile(`^[A-Za-z0-9_-]+/[A-Za-z0-9_-]+/[A-Za-z0-9_-]+$`)

func terraformSites(baseDir string) ([]engine.Site, error) {
	var files []string
	err := filepath.WalkDir(baseDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if path != baseDir && (d.Name() == ".terraform" || d.Name() == ".git" || d.Name() == "node_modules") {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasSuffix(d.Name(), ".tf") {
			files = append(files, path)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	var out []engine.Site
	for _, file := range files {
		content, err := readFile(file)
		if err != nil {
			return nil, err
		}
		out = append(out, terraformFileSites(file, strings.Split(content, "\n"))...)
	}
	return out, nil
}

// tfBlock is one provider entry or module block: its opening line, closing
// line, and what it names.
type tfBlock struct {
	start, end int
	kind, name string
}

func terraformFileSites(file string, lines []string) []engine.Site {
	var blocks []tfBlock
	inReq := false
	for i := 0; i < len(lines); i++ {
		line := lines[i]
		switch {
		case tfReqProv.MatchString(line):
			inReq = true
		case tfModule.MatchString(line):
			end := blockEnd(lines, i)
			blocks = append(blocks, tfBlock{start: i, end: end, kind: "terraform-module"})
			i = end
		case inReq && strings.TrimSpace(line) == "}":
			inReq = false
		case inReq:
			if m := tfEntry.FindStringSubmatch(line); m != nil {
				end := blockEnd(lines, i)
				blocks = append(blocks, tfBlock{start: i, end: end, kind: "terraform-provider", name: "hashicorp/" + m[1]})
				i = end
			}
		}
	}

	var out []engine.Site
	counts := map[string]int{}
	var sites []engine.Site
	for _, b := range blocks {
		verLine := -1
		var source string
		for j := b.start; j <= b.end && j < len(lines); j++ {
			if m := tfSource.FindStringSubmatch(lines[j]); m != nil && source == "" {
				source = m[1]
			}
			if tfVersion.MatchString(lines[j]) && verLine < 0 {
				verLine = j
			}
		}
		if verLine < 0 {
			continue
		}
		name := b.name
		if source != "" {
			name = strings.TrimPrefix(source, "registry.terraform.io/")
		}
		if b.kind == "terraform-module" && !registryModule.MatchString(name) {
			continue
		}
		if b.kind == "terraform-provider" && strings.Count(name, "/") != 1 {
			continue
		}

		// Anchor on every line from the block's opening ("aws = {",
		// `module "vpc" {`), which names this pin alone, to the version
		// line, so the window starts at text unique to it.
		first := b.start
		m := tfVersion.FindStringSubmatchIndex(lines[verLine])
		op := ""
		if m[2] >= 0 {
			op = strings.TrimSpace(lines[verLine][m[2]:m[3]])
		}
		ver := lines[verLine][m[4]:m[5]]
		var pattern strings.Builder
		// Line anchors rather than \A and \z: Find matches a window of
		// exactly this many lines, and Patch runs over the whole file.
		pattern.WriteString(`(?m)^`)
		for j := first; j < verLine; j++ {
			pattern.WriteString(regexp.QuoteMeta(lines[j]) + `\n`)
		}
		pattern.WriteString(regexp.QuoteMeta(lines[verLine][:m[4]]) + `(\d+(?:\.\d+){0,2})` + regexp.QuoteMeta(lines[verLine][m[5]:]) + `$`)

		site := engine.Site{
			Tool:         name,
			ResolverName: name,
			Locator: locator.Locator{
				Glob:     file,
				Pattern:  pattern.String(),
				Resolver: b.kind,
				Window:   verLine - first + 1,
			},
		}
		if op == "~>" {
			site.Constraint = pessimistic(ver)
		}
		counts[pattern.String()]++
		sites = append(sites, site)
	}
	for _, s := range sites {
		n := counts[s.Locator.Pattern]
		if n == 0 {
			continue
		}
		s.Locator.Expect = n
		counts[s.Locator.Pattern] = 0
		out = append(out, s)
	}
	return out
}

// pessimistic is the version range `~> ver` allows: everything but its last
// segment is fixed ("5.0" keeps major 5, "5.0.1" keeps 5.0).
func pessimistic(ver string) *parser.VersionPattern {
	parts := strings.Split(ver, ".")
	if len(parts) == 1 {
		return nil
	}
	fixed := parts[:len(parts)-1]
	for len(fixed) < 3 {
		fixed = append(fixed, "#")
	}
	return parser.ParsePattern(strings.Join(fixed[:3], "."))
}

// blockEnd returns the line closing the brace block opened on line start,
// or start itself for a block that opens and closes on one line.
func blockEnd(lines []string, start int) int {
	depth := 0
	for i := start; i < len(lines); i++ {
		depth += strings.Count(lines[i], "{") - strings.Count(lines[i], "}")
		if depth <= 0 {
			return i
		}
	}
	return len(lines) - 1
}

func readFile(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("reading %s: %w", path, err)
	}
	return string(data), nil
}
