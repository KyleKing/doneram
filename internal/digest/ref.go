// Package digest resolves container image tags to manifest digests through
// the OCI distribution API, so pins can be refreshed without docker installed.
package digest

import (
	"fmt"
	"regexp"
	"strings"
)

// dockerHub is the host that actually serves Docker Hub's registry API;
// "docker.io" and "index.docker.io" are only aliases for it.
const dockerHub = "registry-1.docker.io"

// Ref is a parsed image reference.
type Ref struct {
	// Registry is the API host, with Docker Hub's aliases normalized to
	// registry-1.docker.io.
	Registry string
	// Repository is the full repository path, e.g. "library/python".
	Repository string
	// Tag is empty only for a digest-only reference such as "img@sha256:...".
	Tag string
	// Digest is the pinned digest, if any, e.g. "sha256:...".
	Digest string
	// Name is the image name as the user wrote it, without tag or digest,
	// so rewriting a file never changes "python" into
	// "docker.io/library/python".
	Name string

	// implicitTag records a "latest" the user never wrote, so String does
	// not add one.
	implicitTag bool
}

var (
	pathComponent = `[a-z0-9]+(?:(?:[._]|__|-+)[a-z0-9]+)*`
	repoPattern   = regexp.MustCompile(`^` + pathComponent + `(?:/` + pathComponent + `)*$`)
	tagPattern    = regexp.MustCompile(`^[\w][\w.-]{0,127}$`)
	digestPattern = regexp.MustCompile(`^[a-z0-9]+(?:[+._-][a-z0-9]+)*:[a-zA-Z0-9=_-]{32,}$`)
	hostPattern   = regexp.MustCompile(`^[a-zA-Z0-9](?:[a-zA-Z0-9.-]*[a-zA-Z0-9])?(?::[0-9]+)?$`)
)

// Parse splits an image reference as written in a Dockerfile FROM line or a
// compose image: field. It follows docker's rules for telling a registry
// host from the first path component: the component must contain a "." or
// ":" or be "localhost".
func Parse(image string) (Ref, error) {
	s := strings.TrimSpace(image)
	if s == "" {
		return Ref{}, fmt.Errorf("parsing image %q: empty reference", image)
	}

	var ref Ref
	if name, dgst, ok := strings.Cut(s, "@"); ok {
		if !digestPattern.MatchString(dgst) {
			return Ref{}, fmt.Errorf("parsing image %q: invalid digest %q", image, dgst)
		}
		ref.Digest = dgst
		s = name
	}

	// A colon after the last slash starts the tag; an earlier one is a
	// registry port.
	if i := strings.LastIndex(s, ":"); i > strings.LastIndex(s, "/") {
		ref.Tag = s[i+1:]
		s = s[:i]
		if !tagPattern.MatchString(ref.Tag) {
			return Ref{}, fmt.Errorf("parsing image %q: invalid tag %q", image, ref.Tag)
		}
	}
	ref.Name = s

	repo := s
	ref.Registry = dockerHub
	if first, rest, ok := strings.Cut(s, "/"); ok && isRegistryHost(first) {
		if !hostPattern.MatchString(first) {
			return Ref{}, fmt.Errorf("parsing image %q: invalid registry %q", image, first)
		}
		ref.Registry = normalizeRegistry(first)
		repo = rest
	}
	if ref.Registry == dockerHub && !strings.Contains(repo, "/") {
		repo = "library/" + repo
	}
	if !repoPattern.MatchString(repo) {
		return Ref{}, fmt.Errorf("parsing image %q: invalid repository %q", image, repo)
	}
	ref.Repository = repo

	if ref.Tag == "" && ref.Digest == "" {
		ref.Tag = "latest"
		ref.implicitTag = true
	}
	return ref, nil
}

func isRegistryHost(component string) bool {
	return strings.ContainsAny(component, ".:") || component == "localhost"
}

func normalizeRegistry(host string) string {
	switch host {
	case "docker.io", "index.docker.io":
		return dockerHub
	}
	return host
}

// String renders the reference using the name as originally written, e.g.
// "python:3.13-slim@sha256:...".
func (r Ref) String() string {
	var b strings.Builder
	b.WriteString(r.name())
	if r.Tag != "" && !r.implicitTag {
		b.WriteString(":")
		b.WriteString(r.Tag)
	}
	if r.Digest != "" {
		b.WriteString("@")
		b.WriteString(r.Digest)
	}
	return b.String()
}

// name falls back to the shortest conventional spelling for a Ref built by
// hand rather than by Parse.
func (r Ref) name() string {
	if r.Name != "" {
		return r.Name
	}
	if r.Registry == dockerHub || r.Registry == "" {
		return strings.TrimPrefix(r.Repository, "library/")
	}
	return r.Registry + "/" + r.Repository
}

// display names the image in errors by what was asked for, not by any
// digest it is already pinned to.
func (r Ref) display() string {
	if r.Tag == "" {
		return r.String()
	}
	return r.name() + ":" + r.Tag
}
