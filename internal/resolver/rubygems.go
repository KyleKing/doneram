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

type RubyGemsResolver struct {
	client  *http.Client
	baseURL string
}

func NewRubyGemsResolver(client *http.Client) *RubyGemsResolver {
	return &RubyGemsResolver{
		client:  client,
		baseURL: "https://rubygems.org",
	}
}

func NewRubyGemsResolverWithBaseURL(client *http.Client, baseURL string) *RubyGemsResolver {
	return &RubyGemsResolver{
		client:  client,
		baseURL: baseURL,
	}
}

func (r *RubyGemsResolver) Name() string {
	return "rubygems"
}

type rubyGemsVersion struct {
	Number     string `json:"number"`
	Prerelease bool   `json:"prerelease"`
}

func (r *RubyGemsResolver) Resolve(ctx context.Context, gem string, pattern *parser.VersionPattern) (string, error) {
	logger := httpclient.LoggerFromContext(ctx)
	logger.Debug("resolving gem", "resolver", "rubygems", "gem", gem)

	url := fmt.Sprintf("%s/api/v1/versions/%s.json", r.baseURL, gem)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return "", fmt.Errorf("creating request: %w", err)
	}

	resp, err := r.client.Do(req)
	if err != nil {
		logger.Warn("failed to fetch RubyGems data", "gem", gem, "error", err)
		return "", fmt.Errorf("fetching RubyGems data: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("RubyGems unavailable (status %d) for gem %s: retry later", resp.StatusCode, gem)
	}

	var data []rubyGemsVersion
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return "", fmt.Errorf("decoding RubyGems response: %w", err)
	}

	versions := make([]string, 0)
	for _, v := range data {
		if !v.Prerelease {
			versions = append(versions, normalizeGemVersion(v.Number))
		}
	}

	if len(versions) == 0 {
		return "", fmt.Errorf("no stable versions found for gem %s", gem)
	}

	sort.Slice(versions, func(i, j int) bool {
		return version.Compare(version.Parse(versions[i]), version.Parse(versions[j])) < 0
	})

	for i := len(versions) - 1; i >= 0; i-- {
		if pattern.Matches(versions[i]) {
			logger.Info("resolved gem", "resolver", "rubygems", "gem", gem, "version", versions[i])
			return versions[i], nil
		}
	}

	return "", fmt.Errorf("no matching version found for gem %s with pattern %v", gem, pattern)
}

func normalizeGemVersion(v string) string {
	if idx := strings.Index(v, ".pre"); idx != -1 {
		return v[:idx]
	}
	if idx := strings.Index(v, ".rc"); idx != -1 {
		return v[:idx]
	}
	return v
}

func (r *RubyGemsResolver) GetChangelog(ctx context.Context, pkg string, from, to string) (string, error) {
	return "", nil
}
