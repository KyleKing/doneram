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

type PyPIResolver struct {
	client *http.Client
}

func NewPyPIResolver() *PyPIResolver {
	return &PyPIResolver{
		client: &http.Client{},
	}
}

func (r *PyPIResolver) Name() string {
	return "pypi"
}

type pypiResponse struct {
	Releases map[string][]interface{} `json:"releases"`
}

func (r *PyPIResolver) Resolve(ctx context.Context, pkg string, pattern *parser.VersionPattern) (string, error) {
	url := fmt.Sprintf("https://pypi.org/pypi/%s/json", pkg)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return "", fmt.Errorf("creating request: %w", err)
	}

	resp, err := r.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("fetching PyPI data: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("PyPI returned status %d", resp.StatusCode)
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
			return versions[i], nil
		}
	}

	return "", fmt.Errorf("no matching version found")
}

func (r *PyPIResolver) GetChangelog(ctx context.Context, pkg string, from, to string) (string, error) {
	return "", nil
}
