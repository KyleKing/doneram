package resolver

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/kyleking/doneram/internal/httpclient"
	"github.com/kyleking/doneram/internal/parser"
	"github.com/kyleking/doneram/pkg/version"
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
	TagName     string    `json:"tag_name"`
	Prerelease  bool      `json:"prerelease"`
	Draft       bool      `json:"draft"`
	PublishedAt time.Time `json:"published_at"`
}

func (r *GitHubReleaseResolver) Resolve(ctx context.Context, repo string, pattern *parser.VersionPattern) (string, error) {
	release, err := r.latestMatchingRelease(ctx, repo, pattern)
	if err != nil {
		return "", err
	}
	return strings.TrimPrefix(release.TagName, "v"), nil
}

// latestMatchingRelease returns the newest stable release matching pattern,
// tag name intact (e.g. "v4.3.0"), for a caller that needs the raw tag
// rather than the version Resolve trims it down to.
func (r *GitHubReleaseResolver) latestMatchingRelease(ctx context.Context, repo string, pattern *parser.VersionPattern) (githubRelease, error) {
	logger := httpclient.LoggerFromContext(ctx)
	owner, name, err := splitOwnerRepo(repo)
	if err != nil {
		return githubRelease{}, err
	}
	logger.Debug("resolving repo", "resolver", "github-release", "repo", repo)

	url := fmt.Sprintf("%s/repos/%s/%s/releases?per_page=30", r.baseURL, owner, name)
	releases, err := getGitHubJSON[[]githubRelease](ctx, r.client, url)
	if err != nil {
		logger.Warn("failed to fetch releases", "repo", repo, "error", err)
		return githubRelease{}, fmt.Errorf("fetching releases for %s: %w", repo, err)
	}

	cutoff := cooldownCutoff(ctx)
	if held, ok := heldByCooldown(releases, pattern, cutoff); ok {
		logger.Info("release held by cooldown", "resolver", "github-release", "repo", repo, "version", held.TagName, "published", held.PublishedAt)
	}

	if best, bestTag, ok := bestRelease(releases, pattern, cutoff); ok {
		logger.Info("resolved repo", "resolver", "github-release", "repo", repo, "version", bestTag)
		return best, nil
	}

	// Some projects (mdformat, shellcheck-py, mirrors-prettier) tag every
	// version but never cut a GitHub Release, so /releases comes back
	// empty and the pin has to fall back to /tags.
	tagsURL := fmt.Sprintf("%s/repos/%s/%s/tags?per_page=30", r.baseURL, owner, name)
	tags, err := getGitHubJSON[[]githubTag](ctx, r.client, tagsURL)
	if err != nil {
		logger.Warn("failed to fetch tags", "repo", repo, "error", err)
		return githubRelease{}, fmt.Errorf("no matching stable release found for %s with pattern %v", repo, pattern)
	}

	asReleases := make([]githubRelease, len(tags))
	for i, t := range tags {
		asReleases[i] = githubRelease{TagName: t.Name}
	}
	// A tag carries no publication time, so the cooldown cannot apply to
	// a project that never cuts releases.
	if best, bestTag, ok := bestRelease(asReleases, pattern, time.Time{}); ok {
		logger.Info("resolved repo", "resolver", "github-release", "repo", repo, "version", bestTag, "source", "tags")
		return best, nil
	}

	return githubRelease{}, fmt.Errorf("no matching stable release found for %s with pattern %v", repo, pattern)
}

// bestRelease returns the highest version among releases matching pattern,
// skipping prereleases, drafts, and anything published after cutoff. GitHub
// lists releases by creation date, not version order, so a backported
// release of an old tag can sort above a newer one.
func bestRelease(releases []githubRelease, pattern *parser.VersionPattern, cutoff time.Time) (githubRelease, string, bool) {
	var best githubRelease
	var bestTag string
	found := false
	for _, release := range releases {
		tag := strings.TrimPrefix(release.TagName, "v")
		if release.Prerelease || release.Draft || prereleaseTagPattern.MatchString(tag) {
			continue
		}
		if !pattern.Matches(tag) {
			continue
		}
		if tooNew(release, cutoff) {
			continue
		}
		if !found || version.Compare(version.Parse(tag), version.Parse(bestTag)) > 0 {
			best, bestTag, found = release, tag, true
		}
	}
	return best, bestTag, found
}

func tooNew(release githubRelease, cutoff time.Time) bool {
	return !cutoff.IsZero() && !release.PublishedAt.IsZero() && release.PublishedAt.After(cutoff)
}

// heldByCooldown reports the release a run would have offered had the
// cooldown not applied, so the report can say why the pin stayed put.
func heldByCooldown(releases []githubRelease, pattern *parser.VersionPattern, cutoff time.Time) (githubRelease, bool) {
	if cutoff.IsZero() {
		return githubRelease{}, false
	}
	unheld, unheldTag, ok := bestRelease(releases, pattern, time.Time{})
	if !ok {
		return githubRelease{}, false
	}
	_, heldTag, _ := bestRelease(releases, pattern, cutoff)
	if unheldTag == heldTag {
		return githubRelease{}, false
	}
	return unheld, true
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
