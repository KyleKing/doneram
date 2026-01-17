package resolver

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"sort"
	"strings"

	"github.com/kyleking/doner/internal/parser"
	"github.com/kyleking/doner/pkg/version"
)

type APKResolver struct {
	client *http.Client
}

func NewAPKResolver() *APKResolver {
	return &APKResolver{
		client: &http.Client{},
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
	url := fmt.Sprintf("https://repology.org/api/v1/project/%s", pkg)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return "", fmt.Errorf("creating request: %w", err)
	}

	resp, err := r.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("fetching Repology data: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("Repology returned status %d", resp.StatusCode)
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
		return "", fmt.Errorf("no Alpine versions found")
	}

	sort.Slice(alpineVersions, func(i, j int) bool {
		return version.Compare(version.Parse(alpineVersions[i]), version.Parse(alpineVersions[j])) < 0
	})

	for i := len(alpineVersions) - 1; i >= 0; i-- {
		if pattern.Matches(alpineVersions[i]) {
			return alpineVersions[i], nil
		}
	}

	return "", fmt.Errorf("no matching version found")
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
