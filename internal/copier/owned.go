package copier

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

// A project generated from a template has two kinds of files: those the
// template keeps maintaining, which a `copier update` re-renders, and those
// the project took over (everything the template never rendered, plus the
// template's `_skip_if_exists` files, which it renders once and never
// again). A version pinned in a file of the first kind is the template's to
// move: patched in the project, it turns the next `copier update` into a
// merge, while bumped in the template it reaches every project with one
// release. Owned tells the two apart.

// Settings is the part of a template's copier.yml that decides where its
// files land in a project.
type Settings struct {
	Subdirectory   string
	SkipIfExists   []string
	TemplateSuffix string
}

// ParseSettings reads copier.yml.
func ParseSettings(data []byte) (Settings, error) {
	var raw struct {
		Subdirectory   string    `yaml:"_subdirectory"`
		SkipIfExists   yaml.Node `yaml:"_skip_if_exists"`
		TemplateSuffix *string   `yaml:"_templates_suffix"`
	}
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return Settings{}, err
	}
	s := Settings{Subdirectory: strings.Trim(raw.Subdirectory, "/"), TemplateSuffix: ".jinja"}
	if raw.TemplateSuffix != nil {
		s.TemplateSuffix = *raw.TemplateSuffix
	}
	switch raw.SkipIfExists.Kind {
	case yaml.ScalarNode:
		s.SkipIfExists = []string{raw.SkipIfExists.Value}
	case yaml.SequenceNode:
		for _, n := range raw.SkipIfExists.Content {
			s.SkipIfExists = append(s.SkipIfExists, n.Value)
		}
	}
	return s, nil
}

var (
	// ifBlock is a conditional path segment, `{% if flag %}name{% endif %}`,
	// which renders as name when the flag is set. A project that has the
	// file had the flag set, so the name is all that matters.
	ifBlock = regexp.MustCompile(`\{%-?\s*if\s[^%]*%\}(.*?)\{%-?\s*endif\s*-?%\}`)
	// answersVar is the answers file's own name.
	answersVar = regexp.MustCompile(`\{\{\s*_copier_conf\.answers_file\s*\}\}`)
)

// Owned maps a template's file list (repository paths, slash-separated) to
// the project paths, relative to the project root, that the template keeps
// maintaining. A path whose name depends on an answer other than a
// conditional cannot be mapped and is left to the project.
func Owned(files []string, s Settings, answersFile string) []string {
	prefix := ""
	if s.Subdirectory != "" {
		prefix = s.Subdirectory + "/"
	}
	var out []string
	for _, f := range files {
		if !strings.HasPrefix(f, prefix) {
			continue
		}
		rel := strings.TrimPrefix(f, prefix)
		rel = answersVar.ReplaceAllString(rel, answersFile)
		rel = ifBlock.ReplaceAllString(rel, "$1")
		if strings.Contains(rel, "{{") || strings.Contains(rel, "{%") {
			continue
		}
		if s.TemplateSuffix != "" {
			rel = strings.TrimSuffix(rel, s.TemplateSuffix)
		}
		// An empty segment is a conditional directory that rendered nothing.
		if rel == "" || strings.Contains(rel, "//") || strings.HasPrefix(rel, "/") || rel == answersFile {
			continue
		}
		if skipped(rel, s.SkipIfExists) {
			continue
		}
		out = append(out, rel)
	}
	return out
}

// skipped reports whether a project path matches a `_skip_if_exists`
// pattern. copier reads these as gitignore patterns: one without a slash
// matches a name at any depth, and a directory pattern covers its contents.
func skipped(rel string, patterns []string) bool {
	for _, p := range patterns {
		p = strings.TrimPrefix(strings.TrimSuffix(strings.TrimSuffix(p, "/**"), "/"), "/")
		if p == "" {
			continue
		}
		if ok, _ := path.Match(p, rel); ok || strings.HasPrefix(rel, p+"/") {
			return true
		}
		if !strings.Contains(p, "/") {
			for _, seg := range strings.Split(rel, "/") {
				if ok, _ := path.Match(p, seg); ok {
					return true
				}
			}
		}
	}
	return false
}

// Source lists a template's files and reads its copier.yml at a ref.
type Source interface {
	Files(ctx context.Context, t Template) ([]string, []byte, error)
}

// GitHubSource reads a GitHub-hosted template through the REST API.
type GitHubSource struct {
	Client *http.Client
	// Base is the API root, https://api.github.com unless a test says
	// otherwise. The token is only ever sent there.
	Base string
}

// Files lists the template's tree at t.Commit and fetches its copier.yml.
// A local `_src_path` is read from disk as it stands.
func (g GitHubSource) Files(ctx context.Context, t Template) ([]string, []byte, error) {
	if t.Repo == "" {
		return localFiles(t)
	}
	var tree struct {
		Tree []struct {
			Path string `json:"path"`
			Type string `json:"type"`
		} `json:"tree"`
		Truncated bool `json:"truncated"`
	}
	if err := g.get(ctx, fmt.Sprintf("/repos/%s/git/trees/%s?recursive=1", t.Repo, t.Commit), &tree); err != nil {
		return nil, nil, err
	}
	if tree.Truncated {
		return nil, nil, fmt.Errorf("template %s is too large to list", t.Repo)
	}
	var files []string
	for _, e := range tree.Tree {
		if e.Type == "blob" {
			files = append(files, e.Path)
		}
	}
	for _, name := range []string{"copier.yml", "copier.yaml"} {
		var doc struct {
			Content string `json:"content"`
		}
		if !contains(files, name) {
			continue
		}
		if err := g.get(ctx, fmt.Sprintf("/repos/%s/contents/%s?ref=%s", t.Repo, name, t.Commit), &doc); err != nil {
			return nil, nil, err
		}
		data, err := base64.StdEncoding.DecodeString(strings.ReplaceAll(doc.Content, "\n", ""))
		if err != nil {
			return nil, nil, fmt.Errorf("decoding %s: %w", name, err)
		}
		return files, data, nil
	}
	return nil, nil, fmt.Errorf("template %s has no copier.yml at %s", t.Repo, t.Commit)
}

func (g GitHubSource) get(ctx context.Context, p string, into any) error {
	base := g.Base
	if base == "" {
		base = "https://api.github.com"
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+p, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	if token := os.Getenv("GITHUB_TOKEN"); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := g.Client.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("GitHub API status %d for %s", resp.StatusCode, p)
	}
	return json.NewDecoder(io.LimitReader(resp.Body, 32<<20)).Decode(into)
}

// localFiles reads a template checked out beside the project
// (`_src_path: ../my_template`), as its working tree stands.
func localFiles(t Template) ([]string, []byte, error) {
	root := t.Source
	if !filepath.IsAbs(root) {
		root = filepath.Join(filepath.Dir(t.AnswersFile), root)
	}
	info, err := os.Stat(root)
	if err != nil || !info.IsDir() {
		return nil, nil, fmt.Errorf("template %s is not a local directory or a GitHub repository", t.Source)
	}
	var files []string
	err = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		rel, _ := filepath.Rel(root, p)
		files = append(files, filepath.ToSlash(rel))
		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	for _, name := range []string{"copier.yml", "copier.yaml"} {
		if data, err := os.ReadFile(filepath.Join(root, name)); err == nil {
			return files, data, nil
		}
	}
	return nil, nil, fmt.Errorf("template %s has no copier.yml", t.Source)
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}
