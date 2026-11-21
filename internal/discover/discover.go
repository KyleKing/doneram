// Package discover finds the pins a repo carries in well-known shapes, so
// they are checked without a line of config: SHA-pinned GitHub Actions in
// workflows and composite actions, and tool versions in mise.toml and
// .tool-versions. Config sites always win; a discovered site whose lines a
// config site already covers is dropped.
package discover

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/kyleking/doneram/internal/engine"
	"github.com/kyleking/doneram/internal/locator"
)

// actionFiles are the globs, relative to the repo root, that hold `uses:`
// lines.
var actionFiles = []string{
	".github/workflows/*.yml",
	".github/workflows/*.yaml",
	".github/actions/*/action.yml",
	".github/actions/*/action.yaml",
	"action.yml",
	"action.yaml",
}

// actionUse matches `uses: owner/repo[/path]@<sha> # <tag>`. A SHA with no
// version comment cannot be compared or checked against advisories, and a
// tag-pinned action is mutable, so both are left alone.
// The spacing is fixed at the one form the github-action resolver writes
// back, so a patch never rewrites anything but the SHA and tag.
var actionUse = regexp.MustCompile(`uses:\s*([A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+)(?:/[^@\s]*)?@[0-9a-f]{40} # v?\d[\w.]*`)

// Sites returns every discovered site under baseDir, in a stable order.
func Sites(baseDir string) ([]engine.Site, error) {
	actions, err := actionSites(baseDir)
	if err != nil {
		return nil, err
	}
	tools, err := miseSites(baseDir)
	if err != nil {
		return nil, err
	}
	return append(actions, tools...), nil
}

func actionSites(baseDir string) ([]engine.Site, error) {
	var files []string
	for _, g := range actionFiles {
		found, err := filepath.Glob(filepath.Join(baseDir, g))
		if err != nil {
			return nil, err
		}
		files = append(files, found...)
	}
	sort.Strings(files)

	var out []engine.Site
	for _, file := range files {
		counts, err := countActions(file)
		if err != nil {
			return nil, err
		}
		repos := make([]string, 0, len(counts))
		for repo := range counts {
			repos = append(repos, repo)
		}
		sort.Strings(repos)
		for _, repo := range repos {
			out = append(out, engine.Site{
				Tool:         repo,
				ResolverName: repo,
				Locator: locator.Locator{
					Glob:     file,
					Pattern:  ActionPattern(repo),
					Resolver: "github-action",
					Expect:   counts[repo],
				},
			})
		}
	}
	return out, nil
}

// ActionPattern is the locator pattern for every SHA-pinned use of repo,
// including its sub-path actions (github/codeql-action/init), capturing the
// SHA and its tag comment as the single value github-action patches.
func ActionPattern(repo string) string {
	return `uses:\s*` + regexp.QuoteMeta(repo) + `(?:/[^@\s]*)?@([0-9a-f]{40} # v?\d[\w.]*)`
}

func countActions(file string) (map[string]int, error) {
	f, err := os.Open(file)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()

	counts := make(map[string]int)
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		// Commented-out lines count too: the locator sees them, and the
		// expected count has to agree with what it finds.
		for _, m := range actionUse.FindAllStringSubmatch(scanner.Text(), -1) {
			counts[m[1]]++
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("reading %s: %w", file, err)
	}
	return counts, nil
}

// plainPin is a version mise can resolve against and doneram can patch:
// dotted numbers only. "latest", "lts", "ref:", and "path:" are not pins.
var plainPin = regexp.MustCompile(`^\d+(\.\d+)*$`)

var miseLine = regexp.MustCompile(`^\s*("[^"]+"|[A-Za-z0-9_.:/@-]+)\s*=\s*"([^"]*)"\s*(#.*)?$`)

func miseSites(baseDir string) ([]engine.Site, error) {
	var out []engine.Site

	for _, name := range []string{"mise.toml", ".mise.toml"} {
		path := filepath.Join(baseDir, name)
		content, err := os.ReadFile(path)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return nil, err
		}
		section := ""
		for _, line := range strings.Split(string(content), "\n") {
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, "[") {
				section = strings.Trim(trimmed, "[] ")
				continue
			}
			if section != "tools" {
				continue
			}
			m := miseLine.FindStringSubmatch(line)
			if m == nil || !plainPin.MatchString(m[2]) {
				continue
			}
			tool := strings.Trim(m[1], `"`)
			out = append(out, engine.Site{
				Tool:         tool,
				ResolverName: tool,
				Locator: locator.Locator{
					Glob:     path,
					Pattern:  `(?m)^\s*` + regexp.QuoteMeta(m[1]) + `\s*=\s*"([\d.]+)"`,
					Resolver: "mise",
					Expect:   1,
				},
			})
		}
	}

	path := filepath.Join(baseDir, ".tool-versions")
	content, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	for _, line := range strings.Split(string(content), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 || strings.HasPrefix(fields[0], "#") || !plainPin.MatchString(fields[1]) {
			continue
		}
		out = append(out, engine.Site{
			Tool:         fields[0],
			ResolverName: fields[0],
			Locator: locator.Locator{
				Glob:     path,
				Pattern:  `(?m)^` + regexp.QuoteMeta(fields[0]) + `\s+([\d.]+)`,
				Resolver: "mise",
				Expect:   1,
			},
		})
	}
	return out, nil
}

// Merge appends the discovered sites that no config site already covers.
// A discovered site is dropped whole when any line it matches is a line a
// config site matches, or when its tool name is ignored, so a repo that
// declares a pin explicitly is never checked or patched twice.
func Merge(configured, discovered []engine.Site, ignore []string) ([]engine.Site, error) {
	ignored := make(map[string]bool, len(ignore))
	for _, name := range ignore {
		ignored[name] = true
	}

	type spot struct {
		file string
		line int
	}
	covered := make(map[spot]bool)
	for _, s := range configured {
		if s.Locator.Glob == "" {
			continue
		}
		// A config site that fails to match reports itself when it runs.
		matches, err := locator.Find(s.Locator)
		if err != nil {
			continue
		}
		for _, m := range matches {
			abs, _ := filepath.Abs(m.File)
			covered[spot{abs, m.Line}] = true
		}
	}

	out := append([]engine.Site(nil), configured...)
	for _, s := range discovered {
		if ignored[s.Tool] {
			continue
		}
		matches, err := locator.Find(s.Locator)
		if err != nil {
			return nil, err
		}
		overlap := false
		for _, m := range matches {
			abs, _ := filepath.Abs(m.File)
			if covered[spot{abs, m.Line}] {
				overlap = true
				break
			}
		}
		if !overlap && len(matches) > 0 {
			out = append(out, s)
		}
	}
	return out, nil
}
