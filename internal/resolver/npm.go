package resolver

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"

	"github.com/kyleking/doner/internal/parser"
	"github.com/kyleking/doner/pkg/version"
)

type NPMResolver struct {
	client *http.Client
}

func NewNPMResolver() *NPMResolver {
	return &NPMResolver{
		client: &http.Client{},
	}
}

func (r *NPMResolver) Name() string {
	return "npm"
}

type npmResponse struct {
	Versions map[string]interface{} `json:"versions"`
}

func (r *NPMResolver) Resolve(ctx context.Context, pkg string, pattern *parser.VersionPattern) (string, error) {
	url := fmt.Sprintf("https://registry.npmjs.org/%s", pkg)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return "", fmt.Errorf("creating request: %w", err)
	}

	resp, err := r.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("fetching npm data: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("npm returned status %d", resp.StatusCode)
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
			return versions[i], nil
		}
	}

	return "", fmt.Errorf("no matching version found")
}

func (r *NPMResolver) GetChangelog(ctx context.Context, pkg string, from, to string) (string, error) {
	return "", nil
}
