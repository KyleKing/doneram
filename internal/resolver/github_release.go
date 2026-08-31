package resolver

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"regexp"
	"strings"

	"github.com/kyleking/doneram/internal/httpclient"
	"github.com/kyleking/doneram/internal/parser"
)

// GitHubReleaseResolver resolves the latest stable release of an
// "owner/repo" GitHub project, filtering out anything that looks like a
// prerelease even when the project doesn't set GitHub's own flag for it.
type GitHubReleaseResolver struct {
	client  *http.Client
	baseURL string
}

func NewGitHubReleaseResolver(client *http.Client) *GitHubReleaseResolver {
	return &GitHubReleaseResolver{
		client:  client,
		baseURL: "https://api.github.com",
	}
}

func NewGitHubReleaseResolverWithBaseURL(client *http.Client, baseURL string) *GitHubReleaseResolver {
	return &GitHubReleaseResolver{
		client:  client,
		baseURL: baseURL,
	}
}

func (r *GitHubReleaseResolver) Name() string {
	return "github-release"
}

// prereleaseTagPattern catches a prerelease that only shows up in the tag
// name, since some projects don't set GitHub's own prerelease flag.
var prereleaseTagPattern = regexp.MustCompile(`(?i)-(alpha|beta|rc|pre)\d*$`)

type githubRelease struct {
	TagName    string `json:"tag_name"`
	Prerelease bool   `json:"prerelease"`
	Draft      bool   `json:"draft"`
}

func (r *GitHubReleaseResolver) Resolve(ctx context.Context, repo string, pattern *parser.VersionPattern) (string, error) {
	logger := httpclient.LoggerFromContext(ctx)
	owner, name, err := splitOwnerRepo(repo)
	if err != nil {
		return "", err
	}
	logger.Debug("resolving repo", "resolver", "github-release", "repo", repo)

	url := fmt.Sprintf("%s/repos/%s/%s/releases?per_page=30", r.baseURL, owner, name)
	releases, err := getGitHubJSON[[]githubRelease](ctx, r.client, url)
	if err != nil {
		logger.Warn("failed to fetch releases", "repo", repo, "error", err)
		return "", fmt.Errorf("fetching releases for %s: %w", repo, err)
	}

	for _, release := range releases {
		tag := strings.TrimPrefix(release.TagName, "v")
		if release.Prerelease || release.Draft || prereleaseTagPattern.MatchString(tag) {
			continue
		}
		if pattern.Matches(tag) {
			logger.Info("resolved repo", "resolver", "github-release", "repo", repo, "version", tag)
			return tag, nil
		}
	}

	return "", fmt.Errorf("no matching stable release found for %s with pattern %v", repo, pattern)
}

func (r *GitHubReleaseResolver) GetChangelog(ctx context.Context, pkg string, from, to string) (string, error) {
	return "", nil
}

func splitOwnerRepo(s string) (owner, repo string, err error) {
	parts := strings.SplitN(s, "/", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", fmt.Errorf("expected \"owner/repo\", got %q", s)
	}
	return parts[0], parts[1], nil
}

func getGitHubJSON[T any](ctx context.Context, client *http.Client, url string) (T, error) {
	var out T

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return out, fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("Accept", "application/vnd.github.v3+json")
	if token := os.Getenv("GITHUB_TOKEN"); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := client.Do(req)
	if err != nil {
		return out, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return out, fmt.Errorf("GitHub API unavailable (status %d) for %s: retry later", resp.StatusCode, url)
	}

	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return out, fmt.Errorf("decoding GitHub response: %w", err)
	}
	return out, nil
}
