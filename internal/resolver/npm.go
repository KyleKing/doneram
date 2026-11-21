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

type NPMResolver struct {
	client  *http.Client
	baseURL string
	docs    docCache
}

func NewNPMResolver(client *http.Client) *NPMResolver {
	return &NPMResolver{
		client:  client,
		baseURL: "https://registry.npmjs.org",
	}
}

func NewNPMResolverWithBaseURL(client *http.Client, baseURL string) *NPMResolver {
	return &NPMResolver{
		client:  client,
		baseURL: baseURL,
	}
}

func (r *NPMResolver) Name() string {
	return "npm"
}

type npmResponse struct {
	Versions map[string]npmVersion `json:"versions"`
}

type npmVersion struct {
	// Deprecated is the maintainer's message; npm writes it as a string,
	// and older documents sometimes as false.
	Deprecated json.RawMessage `json:"deprecated"`
}

func (v npmVersion) deprecation() (string, bool) {
	var msg string
	if err := json.Unmarshal(v.Deprecated, &msg); err != nil || msg == "" {
		return "", false
	}
	return msg, true
}

func (r *NPMResolver) fetch(ctx context.Context, pkg string) (npmResponse, error) {
	body, err := r.docs.get(ctx, r.client, fmt.Sprintf("%s/%s", r.baseURL, pkg), "npm registry", "package "+pkg, nil)
	if err != nil {
		httpclient.LoggerFromContext(ctx).Warn("failed to fetch npm data", "package", pkg, "error", err)
		return npmResponse{}, err
	}
	return decodeDoc[npmResponse](body, "npm")
}

func (r *NPMResolver) Resolve(ctx context.Context, pkg string, pattern *parser.VersionPattern) (string, error) {
	logger := httpclient.LoggerFromContext(ctx)
	logger.Debug("resolving package", "resolver", "npm", "package", pkg)

	data, err := r.fetch(ctx, pkg)
	if err != nil {
		return "", err
	}

	versions := make([]string, 0, len(data.Versions))
	for v := range data.Versions {
		versions = append(versions, v)
	}

	sort.Slice(versions, func(i, j int) bool {
		return version.Compare(version.Parse(versions[i]), version.Parse(versions[j])) < 0
	})

	// A deprecated version is skipped unless every match is deprecated,
	// which is how npm marks a whole package as deprecated.
	fallback := ""
	for i := len(versions) - 1; i >= 0; i-- {
		if !pattern.Matches(versions[i]) {
			continue
		}
		if _, deprecated := data.Versions[versions[i]].deprecation(); deprecated {
			if fallback == "" {
				fallback = versions[i]
			}
			continue
		}
		logger.Info("resolved package", "resolver", "npm", "package", pkg, "version", versions[i])
		return versions[i], nil
	}
	if fallback != "" {
		return fallback, nil
	}

	return "", fmt.Errorf("no matching version found for package %s with pattern %v", pkg, pattern)
}

// Yanked reports whether the pinned version has been deprecated since.
func (r *NPMResolver) Yanked(ctx context.Context, pkg, v string) (string, bool, error) {
	data, err := r.fetch(ctx, pkg)
	if err != nil {
		return "", false, err
	}
	msg, deprecated := data.Versions[v].deprecation()
	if deprecated {
		msg = "deprecated: " + msg
	}
	return msg, deprecated, nil
}

func (r *NPMResolver) GetChangelog(ctx context.Context, pkg string, from, to string) (string, error) {
	return "", nil
}
