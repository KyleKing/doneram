package resolver

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/kyleking/doneram/internal/parser"
)

// TestResolveHoldsBackAFreshRelease proves the cooldown skips a release
// published inside the window and offers the previous one instead.
func TestResolveHoldsBackAFreshRelease(t *testing.T) {
	releases := []githubRelease{
		{TagName: "v2.1.0", PublishedAt: time.Now().Add(-2 * time.Hour)},
		{TagName: "v2.0.0", PublishedAt: time.Now().Add(-30 * 24 * time.Hour)},
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(releases)
	}))
	defer server.Close()

	res := NewGitHubReleaseResolverWithBaseURL(server.Client(), server.URL)

	got, err := res.Resolve(ContextWithCooldown(context.Background(), DefaultCooldown), "owner/repo", parser.ParsePattern("#.#.#"))
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got != "2.0.0" {
		t.Errorf("Resolve = %q, want 2.0.0 held back from the two-hour-old release", got)
	}

	got, err = res.Resolve(context.Background(), "owner/repo", parser.ParsePattern("#.#.#"))
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got != "2.1.0" {
		t.Errorf("Resolve without a cooldown = %q, want 2.1.0", got)
	}
}

// TestResolveOffersNothingWhenEveryReleaseIsHeld proves the /tags fallback
// does not hand back the release the cooldown just held. A repo with one
// fresh release has nothing older to offer, and its tag is still listed.
func TestResolveOffersNothingWhenEveryReleaseIsHeld(t *testing.T) {
	releases := []githubRelease{{TagName: "v0.3.2", PublishedAt: time.Now().Add(-9 * time.Hour)}}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/tags") {
			_ = json.NewEncoder(w).Encode([]githubTag{{Name: "v0.3.2"}})
			return
		}
		_ = json.NewEncoder(w).Encode(releases)
	}))
	defer server.Close()

	res := NewGitHubReleaseResolverWithBaseURL(server.Client(), server.URL)

	got, err := res.Resolve(ContextWithCooldown(context.Background(), DefaultCooldown), "owner/repo", parser.ParsePattern("#.#.#"))
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got != "" {
		t.Errorf("Resolve = %q, want no version offered while the only release is inside the window", got)
	}

	got, err = res.Resolve(context.Background(), "owner/repo", parser.ParsePattern("#.#.#"))
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got != "0.3.2" {
		t.Errorf("Resolve without a cooldown = %q, want 0.3.2", got)
	}
}

// TestRegistriesHoldBackAFreshRelease applies the same cooldown to npm and
// PyPI, which date each version in their package documents.
func TestRegistriesHoldBackAFreshRelease(t *testing.T) {
	fresh := time.Now().Add(-2 * time.Hour).UTC().Format(time.RFC3339)
	old := time.Now().Add(-30 * 24 * time.Hour).UTC().Format(time.RFC3339)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, "/pypi/"):
			_, _ = w.Write([]byte(`{"releases":{"2.1.0":[{"upload_time_iso_8601":"` + fresh + `"}],"2.0.0":[{"upload_time_iso_8601":"` + old + `"}]}}`))
		default:
			_, _ = w.Write([]byte(`{"versions":{"2.1.0":{},"2.0.0":{}},"time":{"2.1.0":"` + fresh + `","2.0.0":"` + old + `","created":"` + old + `"}}`))
		}
	}))
	defer server.Close()

	held := ContextWithCooldown(context.Background(), DefaultCooldown)
	for name, res := range map[string]Resolver{
		"npm":  NewNPMResolverWithBaseURL(server.Client(), server.URL),
		"pypi": NewPyPIResolverWithBaseURL(server.Client(), server.URL),
	} {
		got, err := res.Resolve(held, "pkg", parser.ParsePattern("#.#.#"))
		if err != nil || got != "2.0.0" {
			t.Errorf("%s with cooldown = %q, %v; want 2.0.0", name, got, err)
		}
		got, err = res.Resolve(context.Background(), "pkg2", parser.ParsePattern("#.#.#"))
		if err != nil || got != "2.1.0" {
			t.Errorf("%s without cooldown = %q, %v; want 2.1.0", name, got, err)
		}
	}
}

func TestGitHubTag(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "/v-repo/releases"):
			_, _ = w.Write([]byte(`[{"tag_name":"v1.3.0"},{"tag_name":"v1.2.0"}]`))
		case strings.Contains(r.URL.Path, "/bare-repo/releases"):
			_, _ = w.Write([]byte(`[]`))
		case strings.Contains(r.URL.Path, "/bare-repo/tags"):
			_, _ = w.Write([]byte(`[{"name":"1.3.0"}]`))
		default:
			_, _ = w.Write([]byte(`[]`))
		}
	}))
	defer server.Close()
	res := NewGitHubReleaseResolverWithBaseURL(server.Client(), server.URL)
	ctx := context.Background()
	for _, c := range []struct{ repo, version, want string }{
		{"o/v-repo", "1.2.0", "v1.2.0"},
		{"o/v-repo", "v1.3.0", "v1.3.0"},
		{"o/bare-repo", "1.3.0", "1.3.0"},
		{"o/bare-repo", "9.9.9", ""},
	} {
		if got, err := res.Tag(ctx, c.repo, c.version); err != nil || got != c.want {
			t.Errorf("Tag(%s, %s) = %q, %v; want %q", c.repo, c.version, got, err, c.want)
		}
	}
}

func TestMiseUpstream(t *testing.T) {
	gh, npm, pypi := &GitHubReleaseResolver{}, &NPMResolver{}, &PyPIResolver{}
	m := &MiseResolver{GitHub: gh, NPM: npm, PyPI: pypi}
	for backend, want := range map[string]struct {
		hist Historian
		pkg  string
	}{
		"aqua:rhysd/actionlint":               {gh, "rhysd/actionlint"},
		"aqua:golangci/golangci-lint/sub":     {gh, "golangci/golangci-lint"},
		"github:kyleking/doneram":             {gh, "kyleking/doneram"},
		"go:github.com/golangci/golines":      {gh, "golangci/golines"},
		"packslip:github.com/jdx/hk":          {gh, "jdx/hk"},
		"pipx:commitizen":                     {pypi, "commitizen"},
		"npm:prettier":                        {npm, "prettier"},
		"core:node":                           {nil, ""},
		"go:golang.org/x/tools/cmd/goimports": {nil, ""},
	} {
		hist, pkg := m.upstream(backend)
		if hist != want.hist || pkg != want.pkg {
			t.Errorf("upstream(%s) = %T %q, want %T %q", backend, hist, pkg, want.hist, want.pkg)
		}
	}
}
