// Package snapshot renders doneram's pins as a GitHub dependency submission
// snapshot. Submitted dependencies get Dependabot alerts for any ecosystem
// the GitHub Advisory Database covers, which reaches the pins Dependabot
// cannot read on its own: actions pinned by SHA, tools in mise.toml, image
// tags, and versions in templates and scripts.
//
// https://docs.github.com/en/rest/dependency-graph/dependency-submission
package snapshot

import (
	"net/url"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/kyleking/doneram/internal/engine"
)

// Snapshot is the request body of POST /repos/{owner}/{repo}/dependency-graph/snapshots.
type Snapshot struct {
	Version  int                 `json:"version"`
	SHA      string              `json:"sha"`
	Ref      string              `json:"ref"`
	Job      Job                 `json:"job"`
	Detector Detector            `json:"detector"`
	Scanned  string              `json:"scanned"`
	Manifest map[string]Manifest `json:"manifests"`
}

type Job struct {
	Correlator string `json:"correlator"`
	ID         string `json:"id"`
}

type Detector struct {
	Name    string `json:"name"`
	Version string `json:"version"`
	URL     string `json:"url"`
}

type Manifest struct {
	Name     string                `json:"name"`
	File     ManifestFile          `json:"file"`
	Resolved map[string]Dependency `json:"resolved"`
}

type ManifestFile struct {
	SourceLocation string `json:"source_location"`
}

type Dependency struct {
	PackageURL   string `json:"package_url"`
	Relationship string `json:"relationship"`
	Scope        string `json:"scope"`
}

// Options carries what the snapshot needs beyond the pins themselves.
type Options struct {
	// Root is the repo root; manifest paths are relative to it.
	Root     string
	SHA      string
	Ref      string
	JobID    string
	Version  string
	Scanned  time.Time
	Detector string
}

// Build renders every resolvable pin in results as a dependency of the file
// it is written in. A pin with no purl type (a mise tool with no upstream
// ecosystem, a SHA tracking a branch) is left out rather than guessed.
func Build(results []engine.SiteResult, opts Options) Snapshot {
	s := Snapshot{
		Version: 0,
		SHA:     opts.SHA,
		Ref:     opts.Ref,
		Job:     Job{Correlator: "doneram", ID: opts.JobID},
		Detector: Detector{
			Name:    "doneram",
			Version: opts.Version,
			URL:     "https://github.com/kyleking/doneram",
		},
		Scanned:  opts.Scanned.UTC().Format(time.RFC3339),
		Manifest: map[string]Manifest{},
	}
	if s.Job.ID == "" {
		s.Job.ID = s.Scanned
	}

	// A site that failed to resolve still pins what it matched on disk,
	// and a rate-limited registry must not drop it from the graph.
	for _, r := range results {
		for _, m := range r.Matches {
			purl, scope, ok := PackageURL(r.Site, m.Value)
			if !ok {
				continue
			}
			rel, err := filepath.Rel(opts.Root, m.File)
			if err != nil || strings.HasPrefix(rel, "..") {
				rel = m.File
			}
			rel = filepath.ToSlash(rel)
			manifest, ok := s.Manifest[rel]
			if !ok {
				manifest = Manifest{Name: rel, File: ManifestFile{SourceLocation: rel}, Resolved: map[string]Dependency{}}
			}
			manifest.Resolved[purl] = Dependency{PackageURL: purl, Relationship: "direct", Scope: scope}
			s.Manifest[rel] = manifest
		}
	}
	return s
}

// ecosystemTypes maps an OSV ecosystem name (a site's Ecosystem override)
// to its purl type.
var ecosystemTypes = map[string]string{
	"PyPI":           "pypi",
	"npm":            "npm",
	"crates.io":      "cargo",
	"RubyGems":       "gem",
	"Packagist":      "composer",
	"Go":             "golang",
	"Maven":          "maven",
	"NuGet":          "nuget",
	"GitHub Actions": "githubactions",
}

// resolverTypes maps a resolver kind to its purl type.
var resolverTypes = map[string]string{
	"pypi":           "pypi",
	"npm":            "npm",
	"cargo":          "cargo",
	"rubygems":       "gem",
	"composer":       "composer",
	"docker":         "docker",
	"dockerhub":      "docker",
	"ghcr":           "docker",
	"github-action":  "githubactions",
	"github-release": "github",
}

// PackageURL returns the purl for a pin and whether it is a development
// dependency ("development") or not ("runtime"). Workflow actions and
// developer tools are development scope.
func PackageURL(site engine.Site, pinned string) (purl, scope string, ok bool) {
	name := site.ResolverName
	if name == "" {
		name = site.Tool
	}
	kind := site.Locator.Resolver

	typ, ok := ecosystemTypes[site.Ecosystem]
	if !ok {
		typ, ok = resolverTypes[kind]
	}
	// A mise tool backed by a Go module ("go:github.com/x/y/cmd/z") is a Go
	// dependency in all but name.
	if !ok && kind == "mise" && strings.HasPrefix(name, "go:") {
		typ, ok, name = "golang", true, strings.TrimPrefix(name, "go:")
	}
	if !ok {
		return "", "", false
	}

	ver := pinned
	scope = "runtime"
	switch typ {
	case "githubactions":
		_, tag, found := strings.Cut(pinned, "# ")
		if !found {
			return "", "", false
		}
		ver = strings.TrimSpace(tag)
		scope = "development"
	case "golang":
		if !strings.HasPrefix(ver, "v") {
			ver = "v" + ver
		}
		// A floating "v2.12" names no module version an advisory range
		// can be checked against.
		if strings.Count(ver, ".") < 2 {
			return "", "", false
		}
		scope = "development"
	case "github":
		name, _, _ = strings.Cut(name, "@")
	case "docker":
		if kind == "ghcr" && !strings.HasPrefix(name, "ghcr.io/") {
			name = "ghcr.io/" + name
		}
	}
	if typ == "githubactions" || typ == "github" {
		// Sub-path actions (github/codeql-action/init) are one package.
		if parts := strings.SplitN(name, "/", 3); len(parts) >= 2 {
			name = parts[0] + "/" + parts[1]
		}
	}
	if ver == "" || strings.ContainsAny(ver, " \t") {
		return "", "", false
	}

	return "pkg:" + typ + "/" + escapeName(typ, name) + "@" + url.PathEscape(ver), scope, true
}

// escapeName percent-encodes each path segment of a purl name, keeping the
// slashes between namespace and name. npm's scope "@" is encoded as %40.
func escapeName(typ, name string) string {
	segments := strings.Split(name, "/")
	for i, seg := range segments {
		segments[i] = url.PathEscape(seg)
		if typ == "npm" {
			segments[i] = strings.ReplaceAll(segments[i], "@", "%40")
		}
	}
	return strings.Join(segments, "/")
}

// Files returns the manifest paths in the snapshot, sorted, for reporting.
func (s Snapshot) Files() []string {
	out := make([]string, 0, len(s.Manifest))
	for f := range s.Manifest {
		out = append(out, f)
	}
	sort.Strings(out)
	return out
}
