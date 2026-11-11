package resolver

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"

	"github.com/kyleking/doneram/internal/httpclient"
	"github.com/kyleking/doneram/internal/parser"
	"github.com/kyleking/doneram/pkg/version"
)

type GHCRResolver struct {
	client  *http.Client
	baseURL string
}

func NewGHCRResolver(client *http.Client) *GHCRResolver {
	return NewGHCRResolverWithBaseURL(client, "https://ghcr.io")
}

func NewGHCRResolverWithBaseURL(client *http.Client, baseURL string) *GHCRResolver {
	return &GHCRResolver{client: client, baseURL: baseURL}
}

func (r *GHCRResolver) Name() string {
	return "ghcr"
}

type ghcrTagsResponse struct {
	Tags []string `json:"tags"`
}

type ghcrTokenResponse struct {
	Token string `json:"token"`
}

var ghcrNextLink = regexp.MustCompile(`<([^>]+)>;\s*rel="next"`)

func (r *GHCRResolver) Resolve(ctx context.Context, image string, pattern *parser.VersionPattern) (string, error) {
	logger := httpclient.LoggerFromContext(ctx)
	logger.Debug("resolving image", "resolver", "ghcr", "image", image)

	image = strings.TrimPrefix(image, "ghcr.io/")

	parts := strings.Split(image, "/")
	if len(parts) < 2 {
		return "", fmt.Errorf("invalid GHCR image format: %s", image)
	}

	namespace := parts[0]
	repo := strings.Join(parts[1:], "/")

	token, err := r.token(ctx, namespace+"/"+repo)
	if err != nil {
		return "", err
	}

	var tags []string
	next := fmt.Sprintf("/v2/%s/%s/tags/list?n=1000", namespace, repo)
	for next != "" {
		page, link, err := r.tagsPage(ctx, r.baseURL+next, token, image)
		if err != nil {
			return "", err
		}
		tags = append(tags, page...)
		next = link
	}
	tagsResp := ghcrTagsResponse{Tags: tags}

	var matchingTags []string
	for _, tag := range tagsResp.Tags {
		if pattern.Matches(tag) {
			matchingTags = append(matchingTags, tag)
		}
	}

	if len(matchingTags) == 0 {
		return "", fmt.Errorf("no matching tags found for image %s with pattern %s", image, pattern.Raw)
	}

	latest := version.Latest(matchingTags)
	logger.Info("resolved image", "resolver", "ghcr", "image", image, "version", latest)
	return latest, nil
}

// token fetches GHCR's anonymous pull token, which even a public image's
// tag list requires.
func (r *GHCRResolver) token(ctx context.Context, repository string) (string, error) {
	url := fmt.Sprintf("%s/token?scope=repository:%s:pull", r.baseURL, repository)
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return "", fmt.Errorf("creating token request: %w", err)
	}
	resp, err := r.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("fetching GHCR token: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("GHCR token unavailable (status %d) for %s", resp.StatusCode, repository)
	}
	var body ghcrTokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return "", fmt.Errorf("parsing GHCR token: %w", err)
	}
	return body.Token, nil
}

// tagsPage returns one page of tags and the path of the next page, empty on
// the last one.
func (r *GHCRResolver) tagsPage(ctx context.Context, url, token, image string) ([]string, string, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, "", fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := r.client.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("fetching tags: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("GHCR unavailable (status %d) for image %s: retry later", resp.StatusCode, image)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, "", fmt.Errorf("reading response: %w", err)
	}
	var page ghcrTagsResponse
	if err := json.Unmarshal(body, &page); err != nil {
		return nil, "", fmt.Errorf("parsing response: %w", err)
	}
	next := ""
	if m := ghcrNextLink.FindStringSubmatch(resp.Header.Get("Link")); m != nil {
		next = m[1]
	}
	return page.Tags, next, nil
}

func (r *GHCRResolver) GetChangelog(ctx context.Context, pkg string, from, to string) (string, error) {
	return "", nil
}
