package resolver

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/kyleking/doner/internal/parser"
	"github.com/kyleking/doner/pkg/version"
)

type GHCRResolver struct {
	client *http.Client
}

func NewGHCRResolver() *GHCRResolver {
	return &GHCRResolver{
		client: &http.Client{},
	}
}

func (r *GHCRResolver) Name() string {
	return "ghcr"
}

type ghcrTagsResponse struct {
	Tags []string `json:"tags"`
}

func (r *GHCRResolver) Resolve(ctx context.Context, image string, pattern *parser.VersionPattern) (string, error) {
	image = strings.TrimPrefix(image, "ghcr.io/")

	parts := strings.Split(image, "/")
	if len(parts) < 2 {
		return "", fmt.Errorf("invalid GHCR image format: %s", image)
	}

	namespace := parts[0]
	repo := strings.Join(parts[1:], "/")

	url := fmt.Sprintf("https://ghcr.io/v2/%s/%s/tags/list", namespace, repo)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return "", fmt.Errorf("creating request: %w", err)
	}

	resp, err := r.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("fetching tags: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("reading response: %w", err)
	}

	var tagsResp ghcrTagsResponse
	if err := json.Unmarshal(body, &tagsResp); err != nil {
		return "", fmt.Errorf("parsing response: %w", err)
	}

	var matchingTags []string
	for _, tag := range tagsResp.Tags {
		if pattern.Matches(tag) {
			matchingTags = append(matchingTags, tag)
		}
	}

	if len(matchingTags) == 0 {
		return "", fmt.Errorf("no matching tags found for pattern %s", pattern.Raw)
	}

	return version.Latest(matchingTags), nil
}

func (r *GHCRResolver) GetChangelog(ctx context.Context, pkg string, from, to string) (string, error) {
	return "", nil
}
