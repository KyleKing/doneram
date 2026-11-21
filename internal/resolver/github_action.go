package resolver

import (
	"context"
	"fmt"
	"net/http"
	"strings"

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

// Pin resolves a specific version to "<sha> # <tag>", trying the tag with a
// leading v first since OSV records fixed versions without one.
func (r *GitHubActionResolver) Pin(ctx context.Context, repo, version string) (string, error) {
	owner, name, err := splitOwnerRepo(repo)
	if err != nil {
		return "", err
	}
	bare := strings.TrimPrefix(version, "v")
	var lastErr error
	for _, tag := range []string{"v" + bare, bare} {
		commit, err := getGitHubJSON[githubCommit](ctx, r.client, fmt.Sprintf("%s/repos/%s/%s/commits/%s", r.baseURL, owner, name, tag))
		if err != nil {
			lastErr = err
			continue
		}
		return fmt.Sprintf("%s # %s", commit.SHA, tag), nil
	}
	return "", fmt.Errorf("resolving %s@%s to a commit: %w", repo, version, lastErr)
}

// PinRef resolves a ref a workflow uses as written (a floating major "v4",
// an exact tag "v4.2.2", or a bare version) to "<sha> # <tag>". A floating
// ref is named in the comment by the most specific release tag pointing at
// the same commit, so advisories and later updates compare against a real
// version; when no such tag exists the ref itself is kept.
func (r *GitHubActionResolver) PinRef(ctx context.Context, repo, ref string) (string, error) {
	owner, name, err := splitOwnerRepo(repo)
	if err != nil {
		return "", err
	}
	commit, err := getGitHubJSON[githubCommit](ctx, r.client, fmt.Sprintf("%s/repos/%s/%s/commits/%s", r.baseURL, owner, name, ref))
	if err != nil {
		return "", fmt.Errorf("resolving %s@%s to a commit: %w", repo, ref, err)
	}
	if strings.Count(ref, ".") >= 2 {
		return fmt.Sprintf("%s # %s", commit.SHA, ref), nil
	}

	tags, err := getGitHubJSON[[]githubTag](ctx, r.client, fmt.Sprintf("%s/repos/%s/%s/tags?per_page=100", r.baseURL, owner, name))
	if err != nil {
		return "", fmt.Errorf("listing tags for %s: %w", repo, err)
	}
	return fmt.Sprintf("%s # %s", commit.SHA, exactTag(tags, ref, commit.SHA)), nil
}

// exactTag picks, among tags on sha that extend ref ("v4" → "v4.2.2"), the
// one with the most version segments, falling back to ref.
func exactTag(tags []githubTag, ref, sha string) string {
	best := ref
	for _, t := range tags {
		if t.Commit.SHA != sha || !strings.HasPrefix(t.Name, ref+".") || prereleaseTagPattern.MatchString(t.Name) {
			continue
		}
		if strings.Count(t.Name, ".") > strings.Count(best, ".") {
			best = t.Name
		}
	}
	return best
}
