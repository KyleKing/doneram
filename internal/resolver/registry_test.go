package resolver

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"testing"

	"github.com/kyleking/doneram/internal/parser"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

func clientReturning(status int, body string) *http.Client {
	return &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: status,
			Body:       io.NopCloser(bytes.NewBufferString(body)),
			Header:     make(http.Header),
			Request:    req,
		}, nil
	})}
}

func clientFailing() *http.Client {
	return &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return nil, errors.New("transport failure")
	})}
}

func TestParseDockerImage(t *testing.T) {
	tests := []struct {
		image         string
		wantNamespace string
		wantRepo      string
	}{
		{"alpine", "library", "alpine"},
		{"kyleking/doneram", "kyleking", "doneram"},
		{"ghcr.io/kyleking/doneram", "ghcr.io/kyleking", "doneram"},
		{"registry.example.com/team/group/app", "registry.example.com/team/group", "app"},
	}

	for _, tt := range tests {
		namespace, repo := parseDockerImage(tt.image)
		if namespace != tt.wantNamespace || repo != tt.wantRepo {
			t.Errorf("parseDockerImage(%q) = (%q, %q), want (%q, %q)",
				tt.image, namespace, repo, tt.wantNamespace, tt.wantRepo)
		}
	}
}

func TestDockerHubResolverResolve(t *testing.T) {
	body := `{"count": 3, "results": [{"name": "3.19.1"}, {"name": "3.20.0"}, {"name": "latest"}]}`
	r := NewDockerHubResolver(clientReturning(http.StatusOK, body))

	got, err := r.Resolve(context.Background(), "alpine", parser.ParsePattern("3.#.#"))
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got != "3.20.0" {
		t.Errorf("Resolve() = %q, want 3.20.0", got)
	}
}

func TestDockerHubResolverErrors(t *testing.T) {
	pattern := parser.ParsePattern("3.#.#")

	cases := map[string]*DockerHubResolver{
		"transport failure": NewDockerHubResolver(clientFailing()),
		"error status":      NewDockerHubResolver(clientReturning(http.StatusServiceUnavailable, "")),
		"malformed body":    NewDockerHubResolver(clientReturning(http.StatusOK, "not json")),
		"no matching tag":   NewDockerHubResolver(clientReturning(http.StatusOK, `{"results": [{"name": "latest"}]}`)),
	}

	for name, r := range cases {
		if _, err := r.Resolve(context.Background(), "alpine", pattern); err == nil {
			t.Errorf("%s: Resolve should return an error", name)
		}
	}
}

func TestGHCRResolverResolve(t *testing.T) {
	body := `{"tags": ["1.0.0", "1.2.3", "edge"]}`
	r := NewGHCRResolver(clientReturning(http.StatusOK, body))

	got, err := r.Resolve(context.Background(), "ghcr.io/kyleking/doneram", parser.ParsePattern("1.#.#"))
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got != "1.2.3" {
		t.Errorf("Resolve() = %q, want 1.2.3", got)
	}
}

func TestGHCRResolverErrors(t *testing.T) {
	pattern := parser.ParsePattern("1.#.#")

	if _, err := NewGHCRResolver(clientReturning(http.StatusOK, `{"tags":[]}`)).
		Resolve(context.Background(), "ghcr.io/doneram", pattern); err == nil {
		t.Error("an image reference without a namespace should be rejected")
	}

	cases := map[string]*GHCRResolver{
		"transport failure": NewGHCRResolver(clientFailing()),
		"error status":      NewGHCRResolver(clientReturning(http.StatusUnauthorized, "")),
		"malformed body":    NewGHCRResolver(clientReturning(http.StatusOK, "not json")),
		"no matching tag":   NewGHCRResolver(clientReturning(http.StatusOK, `{"tags": ["edge"]}`)),
	}

	for name, r := range cases {
		if _, err := r.Resolve(context.Background(), "ghcr.io/kyleking/doneram", pattern); err == nil {
			t.Errorf("%s: Resolve should return an error", name)
		}
	}
}

func TestNormalizeYumVersion(t *testing.T) {
	tests := map[string]string{
		"8.2.0-1":  "8.2.0",
		"7.76.1-2": "7.76.1",
		"8.2.0":    "8.2.0",
	}
	for in, want := range tests {
		if got := normalizeYumVersion(in); got != want {
			t.Errorf("normalizeYumVersion(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestNormalizeGemVersion(t *testing.T) {
	tests := map[string]string{
		"7.1.0.pre1": "7.1.0",
		"7.1.0.rc2":  "7.1.0",
		"7.1.0":      "7.1.0",
	}
	for in, want := range tests {
		if got := normalizeGemVersion(in); got != want {
			t.Errorf("normalizeGemVersion(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestResolversReturnEmptyChangelog(t *testing.T) {
	client := &http.Client{}
	resolvers := []Resolver{
		NewAPKResolver(client),
		NewAPTResolver(client),
		NewCargoResolver(client),
		NewComposerResolver(client),
		NewDockerHubResolver(client),
		NewGHCRResolver(client),
		NewNPMResolver(client),
		NewPyPIResolver(client),
		NewRubyGemsResolver(client),
		NewYumResolver(client),
	}

	for _, r := range resolvers {
		changelog, err := r.GetChangelog(context.Background(), "pkg", "1.0.0", "1.1.0")
		if err != nil {
			t.Errorf("%s: GetChangelog returned %v", r.Name(), err)
		}
		if changelog != "" {
			t.Errorf("%s: GetChangelog returned %q, want an empty string", r.Name(), changelog)
		}
	}
}

func TestBaseURLResolversRejectFailures(t *testing.T) {
	pattern := parser.ParsePattern("1.#.#")

	failing := map[string]Resolver{
		"cargo":    NewCargoResolverWithBaseURL(clientFailing(), "http://example.invalid"),
		"composer": NewComposerResolverWithBaseURL(clientFailing(), "http://example.invalid"),
		"npm":      NewNPMResolverWithBaseURL(clientFailing(), "http://example.invalid"),
		"pypi":     NewPyPIResolverWithBaseURL(clientFailing(), "http://example.invalid"),
		"rubygems": NewRubyGemsResolverWithBaseURL(clientFailing(), "http://example.invalid"),
		"yum":      NewYumResolverWithBaseURL(clientFailing(), "http://example.invalid"),
	}
	for name, r := range failing {
		pkg := "pkg"
		if name == "composer" {
			pkg = "vendor/pkg"
		}
		if _, err := r.Resolve(context.Background(), pkg, pattern); err == nil {
			t.Errorf("%s: Resolve should fail when the transport fails", name)
		}
	}

	malformed := map[string]Resolver{
		"cargo":    NewCargoResolverWithBaseURL(clientReturning(http.StatusOK, "not json"), "http://example.invalid"),
		"composer": NewComposerResolverWithBaseURL(clientReturning(http.StatusOK, "not json"), "http://example.invalid"),
		"rubygems": NewRubyGemsResolverWithBaseURL(clientReturning(http.StatusOK, "not json"), "http://example.invalid"),
	}
	for name, r := range malformed {
		pkg := "pkg"
		if name == "composer" {
			pkg = "vendor/pkg"
		}
		if _, err := r.Resolve(context.Background(), pkg, pattern); err == nil {
			t.Errorf("%s: Resolve should fail on a malformed response body", name)
		}
	}
}

func TestComposerResolverRejectsUnqualifiedPackage(t *testing.T) {
	r := NewComposerResolverWithBaseURL(&http.Client{}, "http://example.invalid")

	if _, err := r.Resolve(context.Background(), "console", parser.ParsePattern("#.#.#")); err == nil {
		t.Error("Resolve should reject a package name without a vendor prefix")
	}
}
