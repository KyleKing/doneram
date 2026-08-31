package resolver

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"

	"github.com/kyleking/doneram/internal/httpclient"
	"github.com/kyleking/doneram/internal/parser"
	"github.com/kyleking/doneram/pkg/version"
)

// CDNJSResolver resolves the latest version of a library on cdnjs. A
// library name may carry a paired GitHub repo as "library@owner/repo",
// which Detail uses to warn when cdnjs disagrees with that repo's releases.
type CDNJSResolver struct {
	client  *http.Client
	baseURL string
	github  *GitHubReleaseResolver
}

func NewCDNJSResolver(client *http.Client) *CDNJSResolver {
	return &CDNJSResolver{
		client:  client,
		baseURL: "https://api.cdnjs.com",
		github:  NewGitHubReleaseResolver(client),
	}
}

func NewCDNJSResolverWithBaseURL(client *http.Client, baseURL string) *CDNJSResolver {
	return &CDNJSResolver{
		client:  client,
		baseURL: baseURL,
		github:  NewGitHubReleaseResolverWithBaseURL(client, baseURL),
	}
}

func (r *CDNJSResolver) Name() string {
	return "cdnjs"
}

type cdnjsLibrary struct {
	Versions []string `json:"versions"`
}

func (r *CDNJSResolver) Resolve(ctx context.Context, pkg string, pattern *parser.VersionPattern) (string, error) {
	logger := httpclient.LoggerFromContext(ctx)
	library, _, _ := splitCDNJSPeer(pkg)
	logger.Debug("resolving library", "resolver", "cdnjs", "library", library)

	versions, err := r.fetchVersions(ctx, library)
	if err != nil {
		logger.Warn("failed to fetch cdnjs versions", "library", library, "error", err)
		return "", fmt.Errorf("fetching cdnjs versions for %s: %w", library, err)
	}

	sort.Slice(versions, func(i, j int) bool {
		return version.Compare(version.Parse(versions[i]), version.Parse(versions[j])) < 0
	})

	for i := len(versions) - 1; i >= 0; i-- {
		if pattern.Matches(versions[i]) {
			logger.Info("resolved library", "resolver", "cdnjs", "library", library, "version", versions[i])
			return versions[i], nil
		}
	}

	return "", fmt.Errorf("no matching version found for library %s with pattern %v", library, pattern)
}

func (r *CDNJSResolver) fetchVersions(ctx context.Context, library string) ([]string, error) {
	url := fmt.Sprintf("%s/libraries/%s?fields=versions", r.baseURL, library)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}

	resp, err := r.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("cdnjs unavailable (status %d) for library %s: retry later", resp.StatusCode, library)
	}

	var data cdnjsLibrary
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return nil, fmt.Errorf("decoding cdnjs response: %w", err)
	}
	return data.Versions, nil
}

func (r *CDNJSResolver) GetChangelog(ctx context.Context, pkg string, from, to string) (string, error) {
	return "", nil
}

// Detail warns when cdnjs's latest version disagrees with the paired
// GitHub repo's latest release, when one is configured for this library.
func (r *CDNJSResolver) Detail(ctx context.Context, pkg string, current, latest string) (string, error) {
	_, repo, ok := splitCDNJSPeer(pkg)
	if !ok {
		return "", nil
	}

	githubLatest, err := r.github.Resolve(ctx, repo, parser.ParsePattern("#.#.#"))
	if err != nil {
		return "", fmt.Errorf("fetching GitHub releases for %s: %w", repo, err)
	}

	if githubLatest != latest {
		return fmt.Sprintf("warning: cdnjs reports %s but %s's latest GitHub release is %s", latest, repo, githubLatest), nil
	}
	return "", nil
}

func splitCDNJSPeer(pkg string) (library, repo string, ok bool) {
	library, repo, found := strings.Cut(pkg, "@")
	return library, repo, found
}
