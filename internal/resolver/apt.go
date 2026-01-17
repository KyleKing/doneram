package resolver

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"

	"github.com/kyleking/doner/internal/parser"
	"github.com/kyleking/doner/pkg/version"
)

type APTResolver struct {
	client *http.Client
}

func NewAPTResolver() *APTResolver {
	return &APTResolver{
		client: &http.Client{},
	}
}

func (r *APTResolver) Name() string {
	return "apt"
}

func (r *APTResolver) Resolve(ctx context.Context, pkg string, pattern *parser.VersionPattern) (string, error) {
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

	debianVersions := make([]string, 0)
	for _, p := range packages {
		if strings.Contains(p.Repo, "debian") || strings.Contains(p.Repo, "ubuntu") {
			debianVersions = append(debianVersions, normalizeDebianVersion(p.Version))
		}
	}

	if len(debianVersions) == 0 {
		return "", fmt.Errorf("no Debian/Ubuntu versions found")
	}

	sort.Slice(debianVersions, func(i, j int) bool {
		return version.Compare(version.Parse(debianVersions[i]), version.Parse(debianVersions[j])) < 0
	})

	for i := len(debianVersions) - 1; i >= 0; i-- {
		if pattern.Matches(debianVersions[i]) {
			return debianVersions[i], nil
		}
	}

	return "", fmt.Errorf("no matching version found")
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
