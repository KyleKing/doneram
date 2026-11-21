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

func TestExactTag(t *testing.T) {
	tag := func(name, sha string) githubTag {
		var g githubTag
		g.Name, g.Commit.SHA = name, sha
		return g
	}
	tags := []githubTag{
		tag("v4", "s1"), tag("v4.2", "s1"), tag("v4.2.2", "s1"), tag("v4.2.3-rc1", "s1"),
		tag("v4.3.0", "s2"), tag("v40.0.0", "s1"),
	}
	if got := exactTag(tags, "v4", "s1"); got != "v4.2.2" {
		t.Errorf("exactTag(v4) = %s, want v4.2.2", got)
	}
	if got := exactTag(tags, "v5", "s9"); got != "v5" {
		t.Errorf("exactTag with no match = %s, want v5", got)
	}
}
