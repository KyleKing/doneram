package resolver

import (
	"context"
	"net/http"
	"testing"

	"github.com/kyleking/doneram/internal/parser"
	"github.com/kyleking/doneram/internal/testutil"
)

func TestGitHubActionResolver_Resolve(t *testing.T) {
	server := testutil.NewMockServer(map[string]http.HandlerFunc{
		"/repos/owner/repo/releases":       testutil.FixtureHandler("api/github/releases.json"),
		"/repos/owner/repo/commits/v3.1.0": testutil.FixtureHandler("api/github/tag_commit.json"),
	})
	defer server.Close()

	r := NewGitHubActionResolverWithBaseURL(&http.Client{}, server.URL)
	got, err := r.Resolve(context.Background(), "owner/repo", parser.ParsePattern("#.#.#"))
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	want := "eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee # v3.1.0"
	if got != want {
		t.Errorf("Resolve() = %q, want %q", got, want)
	}
}

func TestGitHubActionResolver_NoMatchingRelease(t *testing.T) {
	server := testutil.NewMockServer(map[string]http.HandlerFunc{
		"/repos/owner/repo/releases": testutil.FixtureHandler("api/github/releases.json"),
	})
	defer server.Close()

	r := NewGitHubActionResolverWithBaseURL(&http.Client{}, server.URL)
	if _, err := r.Resolve(context.Background(), "owner/repo", parser.ParsePattern("9.#.#")); err == nil {
		t.Error("expected error when no release matches the pattern")
	}
}

func TestGitHubActionResolver_InvalidRepo(t *testing.T) {
	r := NewGitHubActionResolver(&http.Client{})
	if _, err := r.Resolve(context.Background(), "notaslashrepo", parser.ParsePattern("#.#.#")); err == nil {
		t.Error("expected error for a repo without owner/repo shape")
	}
}

func TestGitHubActionResolver_Name(t *testing.T) {
	r := NewGitHubActionResolver(&http.Client{})
	if r.Name() != "github-action" {
		t.Errorf("Name() = %s, want github-action", r.Name())
	}
}
