package resolver

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"sort"
	"strings"

	"github.com/kyleking/doneram/internal/httpclient"
	"github.com/kyleking/doneram/internal/parser"
	"github.com/kyleking/doneram/pkg/version"
)

type YumResolver struct {
	client  *http.Client
	baseURL string
}

func NewYumResolver(client *http.Client) *YumResolver {
	return &YumResolver{
		client:  client,
		baseURL: "https://repology.org",
	}
}

func NewYumResolverWithBaseURL(client *http.Client, baseURL string) *YumResolver {
	return &YumResolver{
		client:  client,
		baseURL: baseURL,
	}
}

func (r *YumResolver) Name() string {
	return "yum"
}

func (r *YumResolver) Resolve(ctx context.Context, pkg string, pattern *parser.VersionPattern) (string, error) {
	logger := httpclient.LoggerFromContext(ctx)
	logger.Debug("resolving package", "resolver", "yum", "package", pkg)

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

	yumVersions := make([]string, 0)
	for _, p := range packages {
		if strings.Contains(p.Repo, "fedora") || strings.Contains(p.Repo, "centos") {
			yumVersions = append(yumVersions, normalizeYumVersion(p.Version))
		}
	}

	if len(yumVersions) == 0 {
		return "", fmt.Errorf("no Fedora/CentOS versions found for package %s", pkg)
	}

	sort.Slice(yumVersions, func(i, j int) bool {
		return version.Compare(version.Parse(yumVersions[i]), version.Parse(yumVersions[j])) < 0
	})

	for i := len(yumVersions) - 1; i >= 0; i-- {
		if pattern.Matches(yumVersions[i]) {
			logger.Info("resolved package", "resolver", "yum", "package", pkg, "version", yumVersions[i])
			return yumVersions[i], nil
		}
	}

	return "", fmt.Errorf("no matching version found for package %s with pattern %v", pkg, pattern)
}

var yumVersionRegex = regexp.MustCompile(`^(.+)-\d+$`)

func normalizeYumVersion(v string) string {
	matches := yumVersionRegex.FindStringSubmatch(v)
	if matches != nil {
		return matches[1]
	}
	return v
}

func (r *YumResolver) GetChangelog(ctx context.Context, pkg string, from, to string) (string, error) {
	return "", nil
}
