package resolver

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/kyleking/doner/internal/httpclient"
	"github.com/kyleking/doner/internal/parser"
	"github.com/kyleking/doner/pkg/version"
)

type DockerHubResolver struct {
	client *http.Client
}

func NewDockerHubResolver(client *http.Client) *DockerHubResolver {
	return &DockerHubResolver{
		client: client,
	}
}

func (r *DockerHubResolver) Name() string {
	return "dockerhub"
}

type dockerHubTagsResponse struct {
	Count   int               `json:"count"`
	Results []dockerHubResult `json:"results"`
}

type dockerHubResult struct {
	Name string `json:"name"`
}

func (r *DockerHubResolver) Resolve(ctx context.Context, image string, pattern *parser.VersionPattern) (string, error) {
	logger := httpclient.LoggerFromContext(ctx)
	logger.Debug("resolving image", "resolver", "dockerhub", "image", image)

	namespace, repo := parseDockerImage(image)

	url := fmt.Sprintf("https://registry.hub.docker.com/v2/repositories/%s/%s/tags?page_size=100", namespace, repo)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return "", fmt.Errorf("creating request: %w", err)
	}

	resp, err := r.client.Do(req)
	if err != nil {
		logger.Warn("failed to fetch tags", "image", image, "error", err)
		return "", fmt.Errorf("fetching tags: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("Docker Hub unavailable (status %d) for image %s: retry later", resp.StatusCode, image)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("reading response: %w", err)
	}

	var tagsResp dockerHubTagsResponse
	if err := json.Unmarshal(body, &tagsResp); err != nil {
		return "", fmt.Errorf("parsing response: %w", err)
	}

	var matchingTags []string
	for _, result := range tagsResp.Results {
		if pattern.Matches(result.Name) {
			matchingTags = append(matchingTags, result.Name)
		}
	}

	if len(matchingTags) == 0 {
		return "", fmt.Errorf("no matching tags found for image %s with pattern %s", image, pattern.Raw)
	}

	latest := version.Latest(matchingTags)
	logger.Info("resolved image", "resolver", "dockerhub", "image", image, "version", latest)
	return latest, nil
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
