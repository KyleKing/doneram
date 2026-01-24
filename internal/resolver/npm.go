package resolver

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"

	"github.com/kyleking/doner/internal/httpclient"
	"github.com/kyleking/doner/internal/parser"
	"github.com/kyleking/doner/pkg/version"
)

type NPMResolver struct {
	client  *http.Client
	baseURL string
}

func NewNPMResolver(client *http.Client) *NPMResolver {
	return &NPMResolver{
		client:  client,
		baseURL: "https://registry.npmjs.org",
	}
}

func NewNPMResolverWithBaseURL(client *http.Client, baseURL string) *NPMResolver {
	return &NPMResolver{
		client:  client,
		baseURL: baseURL,
	}
}

func (r *NPMResolver) Name() string {
	return "npm"
}

type npmResponse struct {
	Versions map[string]interface{} `json:"versions"`
}

func (r *NPMResolver) Resolve(ctx context.Context, pkg string, pattern *parser.VersionPattern) (string, error) {
	logger := httpclient.LoggerFromContext(ctx)
	logger.Debug("resolving package", "resolver", "npm", "package", pkg)

	url := fmt.Sprintf("%s/%s", r.baseURL, pkg)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return "", fmt.Errorf("creating request: %w", err)
	}

	resp, err := r.client.Do(req)
	if err != nil {
		logger.Warn("failed to fetch npm data", "package", pkg, "error", err)
		return "", fmt.Errorf("fetching npm data: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("npm registry unavailable (status %d) for package %s: retry later", resp.StatusCode, pkg)
	}

	var data npmResponse
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return "", fmt.Errorf("decoding npm response: %w", err)
	}

	versions := make([]string, 0, len(data.Versions))
	for v := range data.Versions {
		versions = append(versions, v)
	}

	sort.Slice(versions, func(i, j int) bool {
		return version.Compare(version.Parse(versions[i]), version.Parse(versions[j])) < 0
	})

	for i := len(versions) - 1; i >= 0; i-- {
		if pattern.Matches(versions[i]) {
			logger.Info("resolved package", "resolver", "npm", "package", pkg, "version", versions[i])
			return versions[i], nil
		}
	}

	return "", fmt.Errorf("no matching version found for package %s with pattern %v", pkg, pattern)
}

func (r *NPMResolver) GetChangelog(ctx context.Context, pkg string, from, to string) (string, error) {
	return "", nil
}
