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

type ComposerResolver struct {
	client  *http.Client
	baseURL string
}

func NewComposerResolver(client *http.Client) *ComposerResolver {
	return &ComposerResolver{
		client:  client,
		baseURL: "https://repo.packagist.org",
	}
}

func NewComposerResolverWithBaseURL(client *http.Client, baseURL string) *ComposerResolver {
	return &ComposerResolver{
		client:  client,
		baseURL: baseURL,
	}
}

func (r *ComposerResolver) Name() string {
	return "composer"
}

type composerResponse struct {
	Packages map[string]map[string]interface{} `json:"packages"`
}

func (r *ComposerResolver) Resolve(ctx context.Context, pkg string, pattern *parser.VersionPattern) (string, error) {
	logger := httpclient.LoggerFromContext(ctx)
	logger.Debug("resolving package", "resolver", "composer", "package", pkg)

	parts := strings.SplitN(pkg, "/", 2)
	if len(parts) != 2 {
		return "", fmt.Errorf("invalid composer package format: %s (expected vendor/package)", pkg)
	}

	vendor := parts[0]
	name := parts[1]

	url := fmt.Sprintf("%s/p2/%s/%s.json", r.baseURL, vendor, name)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return "", fmt.Errorf("creating request: %w", err)
	}

	req.Header.Set("User-Agent", "doneram/1.0")

	resp, err := r.client.Do(req)
	if err != nil {
		logger.Warn("failed to fetch Packagist data", "package", pkg, "error", err)
		return "", fmt.Errorf("fetching Packagist data: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("fetching Packagist data: unavailable (status %d) for package %s, retry later", resp.StatusCode, pkg)
	}

	var data composerResponse
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return "", fmt.Errorf("decoding Packagist response: %w", err)
	}

	packageVersions, ok := data.Packages[pkg]
	if !ok {
		return "", fmt.Errorf("package %s not found in response", pkg)
	}

	versions := make([]string, 0)
	for v := range packageVersions {
		if strings.HasPrefix(v, "dev-") {
			continue
		}

		normalized := strings.TrimPrefix(v, "v")
		versions = append(versions, normalized)
	}

	if len(versions) == 0 {
		return "", fmt.Errorf("no stable versions found for package %s", pkg)
	}

	sort.Slice(versions, func(i, j int) bool {
		return version.Compare(version.Parse(versions[i]), version.Parse(versions[j])) < 0
	})

	for i := len(versions) - 1; i >= 0; i-- {
		if pattern.Matches(versions[i]) {
			logger.Info("resolved package", "resolver", "composer", "package", pkg, "version", versions[i])
			return versions[i], nil
		}
	}

	return "", fmt.Errorf("no matching version found for package %s with pattern %v", pkg, pattern)
}

func (r *ComposerResolver) GetChangelog(ctx context.Context, pkg string, from, to string) (string, error) {
	return "", nil
}
