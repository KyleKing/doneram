// Package changelog grades a library's major-version bump by what its own
// release notes say broke and whether this repo uses any of it. A major is
// not risky in itself: what matters is whether a removed or renamed name the
// notes call out appears in the code that would run against the new version.
//
// It finds the package's source repository from its registry metadata
// (PyPI project_urls, npm repository, crates.io repository), reads the
// GitHub release notes of each major the bump crosses, falling back to the
// repository's CHANGELOG, and collects the lines that announce a breaking
// change. Identifiers those lines put in backticks are then searched for in
// the repo: a hit is breaking, breaking lines with no hit need review, and a
// major whose notes announce nothing breaking is safe.
package changelog

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"

	"github.com/kyleking/doneram/pkg/version"
)

// Level matches actionrisk's grades so both read the same in a PR.
type Level string

const (
	Safe     Level = "safe"
	Review   Level = "review"
	Breaking Level = "breaking"
)

// Assessment is the grade and the evidence behind it.
type Assessment struct {
	Level Level
	Notes []string
}

// Supported reports whether a resolver kind's majors can be graded here.
func Supported(kind string) bool {
	return kind == "pypi" || kind == "npm" || kind == "cargo"
}

// IsMajor reports whether to is a later major than from.
func IsMajor(from, to string) bool {
	f, t := version.Parse(strings.TrimPrefix(from, "v")), version.Parse(strings.TrimPrefix(to, "v"))
	return t.Major > f.Major
}

type Client struct {
	client  *http.Client
	urls    map[string]string
	mu      sync.Mutex
	cache   map[string]cached
	srcOnce sync.Once
	src     [][]byte
}

type cached struct {
	body []byte
	err  error
}

// New uses the public registries and GitHub.
func New(client *http.Client) *Client {
	return NewWithBaseURLs(client, map[string]string{
		"pypi":   "https://pypi.org",
		"npm":    "https://registry.npmjs.org",
		"cargo":  "https://crates.io",
		"github": "https://api.github.com",
	})
}

// NewWithBaseURLs overrides the hosts, for tests.
func NewWithBaseURLs(client *http.Client, urls map[string]string) *Client {
	return &Client{client: client, urls: urls, cache: map[string]cached{}}
}

// Assess grades moving name from one version to another, using the source
// files under repoDir to decide whether breaking notes touch this repo.
func (c *Client) Assess(ctx context.Context, kind, name, from, to, repoDir string) (Assessment, error) {
	repo, err := c.sourceRepo(ctx, kind, name)
	if err != nil {
		return Assessment{}, err
	}
	if repo == "" {
		return Assessment{Level: Review, Notes: []string{fmt.Sprintf("%s names no GitHub source repository to read release notes from", name)}}, nil
	}

	f, t := version.Parse(strings.TrimPrefix(from, "v")), version.Parse(strings.TrimPrefix(to, "v"))
	var lines []string
	read := false
	for major := f.Major + 1; major <= t.Major; major++ {
		notes, ok := c.majorNotes(ctx, repo, name, major)
		if !ok {
			continue
		}
		read = true
		for _, l := range breakingLines(notes) {
			lines = append(lines, fmt.Sprintf("v%d: %s", major, l))
		}
	}
	if !read {
		return Assessment{Level: Review, Notes: []string{fmt.Sprintf("no release notes or changelog found in %s for the majors crossed", repo)}}, nil
	}
	if len(lines) == 0 {
		return Assessment{Level: Safe, Notes: []string{fmt.Sprintf("release notes in %s announce nothing breaking", repo)}}, nil
	}

	a := Assessment{Level: Review}
	used := c.usedIdentifiers(repoDir, lines)
	for _, l := range lines {
		if ids := used[l]; len(ids) > 0 {
			a.Level = Breaking
			a.Notes = append(a.Notes, fmt.Sprintf("%s (this repo uses `%s`)", l, strings.Join(ids, "`, `")))
		}
	}
	if a.Level == Review {
		for i, l := range lines {
			if i == 5 {
				a.Notes = append(a.Notes, fmt.Sprintf("… %d more", len(lines)-5))
				break
			}
			a.Notes = append(a.Notes, l)
		}
	}
	return a, nil
}

var githubRepo = regexp.MustCompile(`github\.com[/:]([A-Za-z0-9_.-]+)/([A-Za-z0-9_.-]+?)(?:\.git)?(?:[/#?].*)?$`)

// sourceRepo returns "owner/repo" on GitHub for a package, or "" when its
// metadata names none.
func (c *Client) sourceRepo(ctx context.Context, kind, name string) (string, error) {
	var candidates []string
	switch kind {
	case "pypi":
		var doc struct {
			Info struct {
				HomePage    string            `json:"home_page"`
				ProjectURLs map[string]string `json:"project_urls"`
			} `json:"info"`
		}
		if err := c.getJSON(ctx, fmt.Sprintf("%s/pypi/%s/json", c.urls["pypi"], url.PathEscape(name)), &doc); err != nil {
			return "", err
		}
		for _, key := range []string{"Source", "Source Code", "Repository", "Code", "GitHub", "Homepage", "Changelog"} {
			candidates = append(candidates, doc.Info.ProjectURLs[key])
		}
		for _, v := range doc.Info.ProjectURLs {
			candidates = append(candidates, v)
		}
		candidates = append(candidates, doc.Info.HomePage)
	case "npm":
		var doc struct {
			Repository json.RawMessage `json:"repository"`
		}
		escaped := strings.Replace(url.PathEscape(name), "%40", "@", 1)
		if err := c.getJSON(ctx, fmt.Sprintf("%s/%s/latest", c.urls["npm"], escaped), &doc); err != nil {
			return "", err
		}
		var s string
		var obj struct {
			URL string `json:"url"`
		}
		if json.Unmarshal(doc.Repository, &s) == nil {
			candidates = append(candidates, s)
		} else if json.Unmarshal(doc.Repository, &obj) == nil {
			candidates = append(candidates, obj.URL)
		}
	case "cargo":
		var doc struct {
			Crate struct {
				Repository string `json:"repository"`
			} `json:"crate"`
		}
		if err := c.getJSON(ctx, fmt.Sprintf("%s/api/v1/crates/%s", c.urls["cargo"], url.PathEscape(name)), &doc); err != nil {
			return "", err
		}
		candidates = append(candidates, doc.Crate.Repository)
	default:
		return "", fmt.Errorf("no changelog source for %s", kind)
	}
	for _, cand := range candidates {
		if m := githubRepo.FindStringSubmatch(cand); m != nil && m[1] != "sponsors" {
			return m[1] + "/" + m[2], nil
		}
	}
	return "", nil
}

// majorNotes returns the release notes of a major's first release, trying
// the tag spellings projects use, then the repository's changelog section
// for that major.
func (c *Client) majorNotes(ctx context.Context, repo, name string, major int) (string, bool) {
	short := name[strings.LastIndex(name, "/")+1:]
	for _, tag := range []string{
		fmt.Sprintf("v%d.0.0", major), fmt.Sprintf("%d.0.0", major),
		fmt.Sprintf("%s@%d.0.0", name, major), fmt.Sprintf("%s-v%d.0.0", short, major), fmt.Sprintf("%s@v%d.0.0", short, major),
	} {
		var rel struct {
			Body string `json:"body"`
		}
		if err := c.getJSON(ctx, fmt.Sprintf("%s/repos/%s/releases/tags/%s", c.urls["github"], repo, url.PathEscape(tag)), &rel); err == nil && strings.TrimSpace(rel.Body) != "" {
			return rel.Body, true
		}
	}
	for _, file := range []string{"CHANGELOG.md", "CHANGES.md", "HISTORY.md", "docs/changelog.md", "CHANGELOG.rst", "CHANGES.rst"} {
		body, err := c.get(ctx, fmt.Sprintf("%s/repos/%s/contents/%s", c.urls["github"], repo, file), "application/vnd.github.raw")
		if err != nil {
			continue
		}
		if section := changelogSection(string(body), major); section != "" {
			return section, true
		}
	}
	return "", false
}

var (
	heading   = regexp.MustCompile(`^#{1,4}\s`)
	underline = regexp.MustCompile(`^[=\-~^]{3,}\s*$`)
)

// changelogSection returns the text under the heading that names major's
// first release (`## 3.0.0`, `## [v3.0.0] - 2024-01-01`, `3.0.0 (2024)`
// over an rst underline), up to the next release heading.
func changelogSection(doc string, major int) string {
	release := regexp.MustCompile(fmt.Sprintf(`(^|[^\d.])v?%d\.0(\.0)?([^\d.]|$)`, major))
	lines := strings.Split(doc, "\n")
	start := -1
	for i, l := range lines {
		isHeading := heading.MatchString(l) || (i+1 < len(lines) && underline.MatchString(lines[i+1]))
		if !isHeading || !release.MatchString(l) {
			continue
		}
		start = i + 1
		break
	}
	if start < 0 {
		return ""
	}
	// The section ends at the next heading that names a release, in either
	// markdown or underlined rst form.
	anyRelease := regexp.MustCompile(`v?\d+\.\d+(\.\d+)?`)
	var out []string
	for i := start; i < len(lines) && i < start+400; i++ {
		l := lines[i]
		next := i+1 < len(lines) && underline.MatchString(lines[i+1])
		if (heading.MatchString(l) || next) && anyRelease.MatchString(l) {
			break
		}
		out = append(out, l)
	}
	return strings.Join(out, "\n")
}

var breaking = regexp.MustCompile(`(?i)\b(breaking|removed?|drop(ped|s)?\s+(support|python|node)|no longer|renamed|is now required|incompatible)\b`)

// breakingLines picks the lines of release notes that announce a breaking
// change, trimmed of list markers.
func breakingLines(notes string) []string {
	var out []string
	seen := map[string]bool{}
	for _, l := range strings.Split(notes, "\n") {
		l = strings.TrimSpace(strings.TrimLeft(strings.TrimSpace(l), "-*#> "))
		if l == "" || len(l) > 300 || !breaking.MatchString(l) || seen[l] {
			continue
		}
		// A bare section heading ("Breaking changes") carries no detail.
		if len(strings.Fields(l)) <= 3 && !strings.Contains(l, "`") {
			continue
		}
		seen[l] = true
		out = append(out, l)
	}
	return out
}

var backticked = regexp.MustCompile("`([A-Za-z_][A-Za-z0-9_.]{2,60})(?:\\(\\))?`")

// usedIdentifiers maps each line to the identifiers it backticks that
// appear as a whole word in this repo's source files.
func (c *Client) usedIdentifiers(repoDir string, lines []string) map[string][]string {
	out := map[string][]string{}
	src := c.sources(repoDir)
	for _, l := range lines {
		seen := map[string]bool{}
		for _, m := range backticked.FindAllStringSubmatch(l, -1) {
			id := m[1]
			// The last segment is what code spells: `pkg.module.func`
			// is called as func or module.func.
			last := id[strings.LastIndex(id, ".")+1:]
			if seen[last] || len(last) < 3 {
				continue
			}
			word := regexp.MustCompile(`\b` + regexp.QuoteMeta(last) + `\b`)
			for _, f := range src {
				if word.Match(f) {
					seen[last] = true
					out[l] = append(out[l], last)
					break
				}
			}
		}
	}
	return out
}

var sourceExts = map[string]bool{
	".py": true, ".pyi": true, ".js": true, ".jsx": true, ".ts": true, ".tsx": true, ".mjs": true,
	".cjs": true, ".rs": true, ".vue": true, ".svelte": true,
}

// sources reads the repo's source files once, capped so a huge repo costs a
// bounded amount of memory.
func (c *Client) sources(repoDir string) [][]byte {
	c.srcOnce.Do(func() {
		total := 0
		_ = filepath.WalkDir(repoDir, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if d.IsDir() {
				switch d.Name() {
				case ".git", "node_modules", "vendor", ".venv", "venv", "target", "dist", "build":
					return filepath.SkipDir
				}
				return nil
			}
			if !sourceExts[filepath.Ext(path)] || total > 64<<20 {
				return nil
			}
			data, err := os.ReadFile(path)
			if err == nil && !bytes.Contains(data[:min(len(data), 512)], []byte{0}) {
				c.src = append(c.src, data)
				total += len(data)
			}
			return nil
		})
	})
	return c.src
}

func (c *Client) getJSON(ctx context.Context, u string, into any) error {
	body, err := c.get(ctx, u, "application/json")
	if err != nil {
		return err
	}
	return json.Unmarshal(body, into)
}

func (c *Client) get(ctx context.Context, u, accept string) ([]byte, error) {
	c.mu.Lock()
	hit, ok := c.cache[u]
	c.mu.Unlock()
	if ok {
		return hit.body, hit.err
	}
	body, err := c.fetch(ctx, u, accept)
	c.mu.Lock()
	c.cache[u] = cached{body, err}
	c.mu.Unlock()
	return body, err
}

func (c *Client) fetch(ctx context.Context, u, accept string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", accept)
	if strings.HasPrefix(u, c.urls["github"]) {
		if token := os.Getenv("GITHUB_TOKEN"); token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
	}
	if strings.HasPrefix(u, c.urls["cargo"]) {
		// crates.io refuses requests without a user agent.
		req.Header.Set("User-Agent", "doneram (https://github.com/kyleking/doneram)")
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("status %d for %s", resp.StatusCode, u)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 4<<20))
}
