package resolver

import (
	"context"
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
	docs    docCache
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

func (r *CargoResolver) fetch(ctx context.Context, crate string) (cargoResponse, error) {
	header := http.Header{"User-Agent": []string{"doneram/1.0"}}
	body, err := r.docs.get(ctx, r.client, fmt.Sprintf("%s/api/v1/crates/%s/versions", r.baseURL, crate), "crates.io", "crate "+crate, header)
	if err != nil {
		httpclient.LoggerFromContext(ctx).Warn("failed to fetch crates.io data", "crate", crate, "error", err)
		return cargoResponse{}, err
	}
	return decodeDoc[cargoResponse](body, "crates.io")
}

func (r *CargoResolver) Resolve(ctx context.Context, crate string, pattern *parser.VersionPattern) (string, error) {
	logger := httpclient.LoggerFromContext(ctx)
	logger.Debug("resolving crate", "resolver", "cargo", "crate", crate)

	data, err := r.fetch(ctx, crate)
	if err != nil {
		return "", err
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

// Yanked reports whether the pinned version has been yanked since.
func (r *CargoResolver) Yanked(ctx context.Context, crate, v string) (string, bool, error) {
	data, err := r.fetch(ctx, crate)
	if err != nil {
		return "", false, err
	}
	for _, cv := range data.Versions {
		if cv.Num == v {
			return "", cv.Yanked, nil
		}
	}
	return "", false, nil
}

func (r *CargoResolver) GetChangelog(ctx context.Context, pkg string, from, to string) (string, error) {
	return "", nil
}
