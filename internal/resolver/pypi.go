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

type PyPIResolver struct {
	client  *http.Client
	baseURL string
}

func NewPyPIResolver(client *http.Client) *PyPIResolver {
	return &PyPIResolver{
		client:  client,
		baseURL: "https://pypi.org",
	}
}

func NewPyPIResolverWithBaseURL(client *http.Client, baseURL string) *PyPIResolver {
	return &PyPIResolver{
		client:  client,
		baseURL: baseURL,
	}
}

func (r *PyPIResolver) Name() string {
	return "pypi"
}

type pypiResponse struct {
	Releases map[string][]interface{} `json:"releases"`
}

func (r *PyPIResolver) Resolve(ctx context.Context, pkg string, pattern *parser.VersionPattern) (string, error) {
	logger := httpclient.LoggerFromContext(ctx)
	logger.Debug("resolving package", "resolver", "pypi", "package", pkg)

	url := fmt.Sprintf("%s/pypi/%s/json", r.baseURL, pkg)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return "", fmt.Errorf("creating request: %w", err)
	}

	resp, err := r.client.Do(req)
	if err != nil {
		logger.Warn("failed to fetch PyPI data", "package", pkg, "error", err)
		return "", fmt.Errorf("fetching PyPI data: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("PyPI registry unavailable (status %d) for package %s: retry later", resp.StatusCode, pkg)
	}

	var data pypiResponse
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return "", fmt.Errorf("decoding PyPI response: %w", err)
	}

	versions := make([]string, 0, len(data.Releases))
	for v := range data.Releases {
		versions = append(versions, v)
	}

	sort.Slice(versions, func(i, j int) bool {
		return version.Compare(version.Parse(versions[i]), version.Parse(versions[j])) < 0
	})

	for i := len(versions) - 1; i >= 0; i-- {
		if pattern.Matches(versions[i]) {
			logger.Info("resolved package", "resolver", "pypi", "package", pkg, "version", versions[i])
			return versions[i], nil
		}
	}

	return "", fmt.Errorf("no matching version found for package %s with pattern %v", pkg, pattern)
}

func (r *PyPIResolver) GetChangelog(ctx context.Context, pkg string, from, to string) (string, error) {
	return "", nil
}
