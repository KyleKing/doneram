package resolver

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"sort"
	"strings"

	"github.com/kyleking/doner/internal/httpclient"
	"github.com/kyleking/doner/internal/parser"
	"github.com/kyleking/doner/pkg/version"
)

type APKResolver struct {
	client  *http.Client
	baseURL string
}

func NewAPKResolver(client *http.Client) *APKResolver {
	return &APKResolver{
		client:  client,
		baseURL: "https://repology.org",
	}
}

func NewAPKResolverWithBaseURL(client *http.Client, baseURL string) *APKResolver {
	return &APKResolver{
		client:  client,
		baseURL: baseURL,
	}
}

func (r *APKResolver) Name() string {
	return "apk"
}

type repologyPackage struct {
	Version string `json:"version"`
	Status  string `json:"status"`
	Repo    string `json:"repo"`
}

func (r *APKResolver) Resolve(ctx context.Context, pkg string, pattern *parser.VersionPattern) (string, error) {
	logger := httpclient.LoggerFromContext(ctx)
	logger.Debug("resolving package", "resolver", "apk", "package", pkg)

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

	alpineVersions := make([]string, 0)
	for _, p := range packages {
		if strings.Contains(p.Repo, "alpine") {
			alpineVersions = append(alpineVersions, normalizeAPKVersion(p.Version))
		}
	}

	if len(alpineVersions) == 0 {
		return "", fmt.Errorf("no Alpine versions found for package %s", pkg)
	}

	sort.Slice(alpineVersions, func(i, j int) bool {
		return version.Compare(version.Parse(alpineVersions[i]), version.Parse(alpineVersions[j])) < 0
	})

	for i := len(alpineVersions) - 1; i >= 0; i-- {
		if pattern.Matches(alpineVersions[i]) {
			logger.Info("resolved package", "resolver", "apk", "package", pkg, "version", alpineVersions[i])
			return alpineVersions[i], nil
		}
	}

	return "", fmt.Errorf("no matching version found for package %s with pattern %v", pkg, pattern)
}

var apkVersionRegex = regexp.MustCompile(`^(.+)-r\d+$`)

func normalizeAPKVersion(v string) string {
	matches := apkVersionRegex.FindStringSubmatch(v)
	if matches != nil {
		return matches[1]
	}
	return v
}

func (r *APKResolver) GetChangelog(ctx context.Context, pkg string, from, to string) (string, error) {
	return "", nil
}
