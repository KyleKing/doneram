package resolver

import (
	"context"
	"net/http"
	"testing"

	"github.com/kyleking/doneram/internal/parser"
	"github.com/kyleking/doneram/internal/testutil"
)

func TestGitHubReleaseResolver_Resolve(t *testing.T) {
	server := testutil.NewMockServer(map[string]http.HandlerFunc{
		"/repos/owner/repo/releases": testutil.FixtureHandler("api/github/releases.json"),
	})
	defer server.Close()

	r := NewGitHubReleaseResolverWithBaseURL(&http.Client{}, server.URL)
	ctx := context.Background()

	got, err := r.Resolve(ctx, "owner/repo", parser.ParsePattern("#.#.#"))
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if got != "3.1.0" {
		t.Errorf("Resolve() = %q, want 3.1.0 (prereleases and rc tags skipped)", got)
	}
}

func TestGitHubReleaseResolver_IgnoresBackportedOldTagListedFirst(t *testing.T) {
	server := testutil.NewMockServer(map[string]http.HandlerFunc{
		"/repos/owner/repo/releases": testutil.FixtureHandler("api/github/releases_backported.json"),
	})
	defer server.Close()

	r := NewGitHubReleaseResolverWithBaseURL(&http.Client{}, server.URL)
	got, err := r.Resolve(context.Background(), "owner/repo", parser.ParsePattern("#.#.#"))
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if got != "8.0.1" {
		t.Errorf("Resolve() = %q, want 8.0.1 (a backported v3.1.0 listed first must not win)", got)
	}
}

func TestGitHubReleaseResolver_InvalidRepo(t *testing.T) {
	r := NewGitHubReleaseResolver(&http.Client{})
	if _, err := r.Resolve(context.Background(), "notaslashrepo", parser.ParsePattern("#.#.#")); err == nil {
		t.Error("expected error for a repo without owner/repo shape")
	}
}

func TestGitHubReleaseResolver_NotFound(t *testing.T) {
	server := testutil.NewMockServer(map[string]http.HandlerFunc{
		"/repos/owner/missing/releases": testutil.ErrorHandler(404),
	})
	defer server.Close()

	r := NewGitHubReleaseResolverWithBaseURL(&http.Client{}, server.URL)
	if _, err := r.Resolve(context.Background(), "owner/missing", parser.ParsePattern("#.#.#")); err == nil {
		t.Error("expected error for a missing repo")
	}
}

func TestGitHubReleaseResolver_Name(t *testing.T) {
	r := NewGitHubReleaseResolver(&http.Client{})
	if r.Name() != "github-release" {
		t.Errorf("Name() = %s, want github-release", r.Name())
	}
}
