package resolver

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/kyleking/doneram/internal/parser"
	"github.com/kyleking/doneram/internal/testutil"
)

const (
	testPinnedSHA = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	testHeadSHA   = "cccccccccccccccccccccccccccccccccccccc"
	testTagSHA    = "dddddddddddddddddddddddddddddddddddddd"
)

func TestGitHubBranchResolver_Resolve(t *testing.T) {
	server := testutil.NewMockServer(map[string]http.HandlerFunc{
		"/repos/owner/repo/commits/main": testutil.FixtureHandler("api/github/branch_head.json"),
	})
	defer server.Close()

	r := NewGitHubBranchResolverWithBaseURL(&http.Client{}, server.URL)
	got, err := r.Resolve(context.Background(), "owner/repo@main", parser.ParsePattern("#.#.#"))
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if got != testHeadSHA {
		t.Errorf("Resolve() = %q, want %q", got, testHeadSHA)
	}
}

func TestGitHubBranchResolver_ResolveDefaultsToMain(t *testing.T) {
	server := testutil.NewMockServer(map[string]http.HandlerFunc{
		"/repos/owner/repo/commits/main": testutil.FixtureHandler("api/github/branch_head.json"),
	})
	defer server.Close()

	r := NewGitHubBranchResolverWithBaseURL(&http.Client{}, server.URL)
	if _, err := r.Resolve(context.Background(), "owner/repo", parser.ParsePattern("#.#.#")); err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
}

func TestGitHubBranchResolver_Detail(t *testing.T) {
	server := testutil.NewMockServer(map[string]http.HandlerFunc{
		"/repos/owner/repo/commits/" + testPinnedSHA:                      testutil.FixtureHandler("api/github/pinned_commit.json"),
		"/repos/owner/repo/compare/" + testPinnedSHA + "...main":          testutil.FixtureHandler("api/github/compare.json"),
		"/repos/owner/repo/tags":                                          testutil.FixtureHandler("api/github/tags.json"),
		"/repos/owner/repo/compare/" + testPinnedSHA + "..." + testTagSHA: testutil.FixtureHandler("api/github/compare_tag.json"),
	})
	defer server.Close()

	r := NewGitHubBranchResolverWithBaseURL(&http.Client{}, server.URL)
	detail, err := r.Detail(context.Background(), "owner/repo@main", testPinnedSHA, testHeadSHA)
	if err != nil {
		t.Fatalf("Detail() error = %v", err)
	}

	if !strings.Contains(detail, "8 commits") {
		t.Errorf("Detail() = %q, want commit count", detail)
	}
	if !strings.Contains(detail, "warning") || !strings.Contains(detail, "v2.0.0") {
		t.Errorf("Detail() = %q, want a newer-tag warning naming v2.0.0", detail)
	}
}

func TestGitHubBranchResolver_DetailNoWarningWithoutTags(t *testing.T) {
	server := testutil.NewMockServer(map[string]http.HandlerFunc{
		"/repos/owner/repo/commits/" + testPinnedSHA:             testutil.FixtureHandler("api/github/pinned_commit.json"),
		"/repos/owner/repo/compare/" + testPinnedSHA + "...main": testutil.FixtureHandler("api/github/compare.json"),
		"/repos/owner/repo/tags":                                 testutil.FixtureHandler("api/github/tags_empty.json"),
	})
	defer server.Close()

	r := NewGitHubBranchResolverWithBaseURL(&http.Client{}, server.URL)
	detail, err := r.Detail(context.Background(), "owner/repo@main", testPinnedSHA, testHeadSHA)
	if err != nil {
		t.Fatalf("Detail() error = %v", err)
	}
	if strings.Contains(detail, "warning") {
		t.Errorf("Detail() = %q, want no warning for a repo with no tags", detail)
	}
}

func TestGitHubBranchResolver_DetailNoOpWhenUpToDate(t *testing.T) {
	r := NewGitHubBranchResolver(&http.Client{})
	detail, err := r.Detail(context.Background(), "owner/repo@main", testHeadSHA, testHeadSHA)
	if err != nil || detail != "" {
		t.Errorf("Detail() = (%q, %v), want empty and no error when already up to date", detail, err)
	}
}

func TestGitHubBranchResolver_Name(t *testing.T) {
	r := NewGitHubBranchResolver(&http.Client{})
	if r.Name() != "github-branch" {
		t.Errorf("Name() = %s, want github-branch", r.Name())
	}
}
