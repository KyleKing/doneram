package resolver

import (
	"context"
	"fmt"
	"net/http"

	"github.com/kyleking/doneram/internal/httpclient"
	"github.com/kyleking/doneram/internal/parser"
)

// GitHubActionResolver resolves a GitHub Action `uses: owner/repo@<sha> #
// <tag>` pin. Updating it takes two lookups where every other resolver
// takes one: the latest tag matching the pattern, then that tag's commit
// SHA. Resolve folds both into a single composite value ("<sha> # <tag>")
// so the existing single-capture-group locator pattern and patch can apply
// it structurally, as long as the site's capture group spans the SHA and
// the trailing comment together.
type GitHubActionResolver struct {
	releases *GitHubReleaseResolver
	client   *http.Client
	baseURL  string
}

func NewGitHubActionResolver(client *http.Client) *GitHubActionResolver {
	return NewGitHubActionResolverWithBaseURL(client, "https://api.github.com")
}

func NewGitHubActionResolverWithBaseURL(client *http.Client, baseURL string) *GitHubActionResolver {
	return &GitHubActionResolver{
		releases: NewGitHubReleaseResolverWithBaseURL(client, baseURL),
		client:   client,
		baseURL:  baseURL,
	}
}

func (r *GitHubActionResolver) Name() string {
	return "github-action"
}

func (r *GitHubActionResolver) Resolve(ctx context.Context, repo string, pattern *parser.VersionPattern) (string, error) {
	logger := httpclient.LoggerFromContext(ctx)
	owner, name, err := splitOwnerRepo(repo)
	if err != nil {
		return "", err
	}

	release, err := r.releases.latestMatchingRelease(ctx, repo, pattern)
	if err != nil {
		return "", err
	}
	if release.TagName == "" {
		return "", nil
	}

	commit, err := getGitHubJSON[githubCommit](ctx, r.client, fmt.Sprintf("%s/repos/%s/%s/commits/%s", r.baseURL, owner, name, release.TagName))
	if err != nil {
		return "", fmt.Errorf("fetching commit for %s@%s: %w", repo, release.TagName, err)
	}

	logger.Info("resolved action pin", "resolver", "github-action", "repo", repo, "tag", release.TagName, "sha", commit.SHA)
	return fmt.Sprintf("%s # %s", commit.SHA, release.TagName), nil
}

func (r *GitHubActionResolver) GetChangelog(ctx context.Context, pkg string, from, to string) (string, error) {
	return "", nil
}
