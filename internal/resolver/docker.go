package resolver

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/kyleking/doneram/internal/httpclient"
	"github.com/kyleking/doneram/internal/parser"
	"github.com/kyleking/doneram/pkg/version"
)

type DockerHubResolver struct {
	client  *http.Client
	baseURL string
}

func NewDockerHubResolver(client *http.Client) *DockerHubResolver {
	return &DockerHubResolver{client: client, baseURL: "https://registry.hub.docker.com"}
}

func (r *DockerHubResolver) Name() string {
	return "dockerhub"
}

type dockerHubTagsResponse struct {
	Count   int               `json:"count"`
	Next    string            `json:"next"`
	Results []dockerHubResult `json:"results"`
}

type dockerHubResult struct {
	Name string `json:"name"`
}

// dockerHubPages bounds how far the tag list is read. A popular image has
// thousands of tags; filtered by the pattern's fixed prefix, the ones a
// pin can move to fit in a few pages.
const dockerHubPages = 10

func (r *DockerHubResolver) Resolve(ctx context.Context, image string, pattern *parser.VersionPattern) (string, error) {
	logger := httpclient.LoggerFromContext(ctx)
	logger.Debug("resolving image", "resolver", "dockerhub", "image", image)

	namespace, repo := parseDockerImage(image)
	// Docker Hub lists tags most recently pushed first, and python alone
	// has thousands, so the list is narrowed to the pattern's fixed leading
	// segments ("3.13") and read past the first page.
	q := url.Values{"page_size": {"100"}}
	if prefix := literalPrefix(pattern); prefix != "" {
		q.Set("name", prefix)
	}
	next := fmt.Sprintf("%s/v2/repositories/%s/%s/tags?%s", r.baseURL, namespace, repo, q.Encode())

	var matchingTags []string
	for page := 0; next != "" && page < dockerHubPages; page++ {
		tagsResp, err := r.page(ctx, next, image)
		if err != nil {
			return "", err
		}
		for _, result := range tagsResp.Results {
			if pattern.Matches(result.Name) {
				matchingTags = append(matchingTags, result.Name)
			}
		}
		next = tagsResp.Next
	}

	if len(matchingTags) == 0 {
		return "", fmt.Errorf("no matching tags found for image %s with pattern %s", image, pattern.Raw)
	}

	latest := version.Latest(matchingTags)
	logger.Info("resolved image", "resolver", "dockerhub", "image", image, "version", latest)
	return latest, nil
}

func (r *DockerHubResolver) page(ctx context.Context, pageURL, image string) (*dockerHubTagsResponse, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", pageURL, nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}
	resp, err := r.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetching tags: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetching tags: Docker Hub unavailable (status %d) for image %s, retry later", resp.StatusCode, image)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading response: %w", err)
	}
	var tagsResp dockerHubTagsResponse
	if err := json.Unmarshal(body, &tagsResp); err != nil {
		return nil, fmt.Errorf("parsing response: %w", err)
	}
	return &tagsResp, nil
}

// literalPrefix is the pattern's fixed leading segments ("3.13.#" gives
// "3.13"), empty when the major is a wildcard. Docker Hub's name filter is
// a substring match, so it only narrows; Matches still decides.
func literalPrefix(p *parser.VersionPattern) string {
	if p == nil {
		return ""
	}
	var fixed []string
	for _, seg := range []string{p.Major, p.Minor, p.Patch} {
		if seg == "" || seg == "#" {
			break
		}
		fixed = append(fixed, seg)
	}
	return strings.Join(fixed, ".")
}

func (r *DockerHubResolver) GetChangelog(ctx context.Context, pkg string, from, to string) (string, error) {
	return "", nil
}

func parseDockerImage(image string) (namespace, repo string) {
	parts := strings.Split(image, "/")

	if len(parts) == 1 {
		return "library", parts[0]
	}

	if len(parts) == 2 {
		return parts[0], parts[1]
	}

	return strings.Join(parts[:len(parts)-1], "/"), parts[len(parts)-1]
}
