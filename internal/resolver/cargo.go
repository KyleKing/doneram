package resolver

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"

	"github.com/kyleking/doneram/internal/httpclient"
	"github.com/kyleking/doneram/internal/parser"
	"github.com/kyleking/doneram/pkg/version"
)

type CargoResolver struct {
	client  *http.Client
	baseURL string
}

func NewCargoResolver(client *http.Client) *CargoResolver {
	return &CargoResolver{
		client:  client,
		baseURL: "https://crates.io",
	}
}

func NewCargoResolverWithBaseURL(client *http.Client, baseURL string) *CargoResolver {
	return &CargoResolver{
		client:  client,
		baseURL: baseURL,
	}
}

func (r *CargoResolver) Name() string {
	return "cargo"
}

type cargoResponse struct {
	Versions []cargoVersion `json:"versions"`
}

type cargoVersion struct {
	Num    string `json:"num"`
	Yanked bool   `json:"yanked"`
}

func (r *CargoResolver) Resolve(ctx context.Context, crate string, pattern *parser.VersionPattern) (string, error) {
	logger := httpclient.LoggerFromContext(ctx)
	logger.Debug("resolving crate", "resolver", "cargo", "crate", crate)

	url := fmt.Sprintf("%s/api/v1/crates/%s/versions", r.baseURL, crate)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return "", fmt.Errorf("creating request: %w", err)
	}

	req.Header.Set("User-Agent", "doneram/1.0")

	resp, err := r.client.Do(req)
	if err != nil {
		logger.Warn("failed to fetch crates.io data", "crate", crate, "error", err)
		return "", fmt.Errorf("fetching crates.io data: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("crates.io unavailable (status %d) for crate %s: retry later", resp.StatusCode, crate)
	}

	var data cargoResponse
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return "", fmt.Errorf("decoding crates.io response: %w", err)
	}

	versions := make([]string, 0)
	for _, v := range data.Versions {
		if !v.Yanked {
			versions = append(versions, v.Num)
		}
	}

	if len(versions) == 0 {
		return "", fmt.Errorf("no non-yanked versions found for crate %s", crate)
	}

	sort.Slice(versions, func(i, j int) bool {
		return version.Compare(version.Parse(versions[i]), version.Parse(versions[j])) < 0
	})

	for i := len(versions) - 1; i >= 0; i-- {
		if pattern.Matches(versions[i]) {
			logger.Info("resolved crate", "resolver", "cargo", "crate", crate, "version", versions[i])
			return versions[i], nil
		}
	}

	return "", fmt.Errorf("no matching version found for crate %s with pattern %v", crate, pattern)
}

func (r *CargoResolver) GetChangelog(ctx context.Context, pkg string, from, to string) (string, error) {
	return "", nil
}
