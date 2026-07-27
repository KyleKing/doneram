package resolver

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"

	"github.com/kyleking/doner/internal/httpclient"
	"github.com/kyleking/doner/internal/parser"
	"github.com/kyleking/doner/pkg/version"
)

type APTResolver struct {
	client  *http.Client
	baseURL string
}

func NewAPTResolver(client *http.Client) *APTResolver {
	return &APTResolver{
		client:  client,
		baseURL: "https://repology.org",
	}
}

func NewAPTResolverWithBaseURL(client *http.Client, baseURL string) *APTResolver {
	return &APTResolver{
		client:  client,
		baseURL: baseURL,
	}
}

func (r *APTResolver) Name() string {
	return "apt"
}

func (r *APTResolver) Resolve(ctx context.Context, pkg string, pattern *parser.VersionPattern) (string, error) {
	logger := httpclient.LoggerFromContext(ctx)
	logger.Debug("resolving package", "resolver", "apt", "package", pkg)

	url := fmt.Sprintf("%s/api/v1/project/%s", r.baseURL, pkg)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return "", fmt.Errorf("creating request: %w", err)
	}

	resp, err := r.client.Do(req)
	if err != nil {
		logger.Warn("failed to fetch Repology data", "package", pkg, "error", err)
		return "", fmt.Errorf("fetching Repology data: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("fetching Repology data: unavailable (status %d) for package %s, retry later", resp.StatusCode, pkg)
	}

	var packages []repologyPackage
	if err := json.NewDecoder(resp.Body).Decode(&packages); err != nil {
		return "", fmt.Errorf("decoding Repology response: %w", err)
	}

	debianVersions := make([]string, 0)
	for _, p := range packages {
		if strings.Contains(p.Repo, "debian") || strings.Contains(p.Repo, "ubuntu") {
			debianVersions = append(debianVersions, normalizeDebianVersion(p.Version))
		}
	}

	if len(debianVersions) == 0 {
		return "", fmt.Errorf("no Debian/Ubuntu versions found for package %s", pkg)
	}

	sort.Slice(debianVersions, func(i, j int) bool {
		return version.Compare(version.Parse(debianVersions[i]), version.Parse(debianVersions[j])) < 0
	})

	for i := len(debianVersions) - 1; i >= 0; i-- {
		if pattern.Matches(debianVersions[i]) {
			logger.Info("resolved package", "resolver", "apt", "package", pkg, "version", debianVersions[i])
			return debianVersions[i], nil
		}
	}

	return "", fmt.Errorf("no matching version found for package %s with pattern %v", pkg, pattern)
}

func normalizeDebianVersion(v string) string {
	if idx := strings.Index(v, "-"); idx != -1 {
		return v[:idx]
	}
	return v
}

func (r *APTResolver) GetChangelog(ctx context.Context, pkg string, from, to string) (string, error) {
	return "", nil
}
