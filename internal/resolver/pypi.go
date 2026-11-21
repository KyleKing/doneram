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

type PyPIResolver struct {
	client  *http.Client
	baseURL string
	docs    docCache
}

func NewPyPIResolver(client *http.Client) *PyPIResolver {
	return &PyPIResolver{
		client:  client,
		baseURL: "https://pypi.org",
	}
}

func NewPyPIResolverWithBaseURL(client *http.Client, baseURL string) *PyPIResolver {
	return &PyPIResolver{
		client:  client,
		baseURL: baseURL,
	}
}

func (r *PyPIResolver) Name() string {
	return "pypi"
}

type pypiResponse struct {
	Releases map[string][]pypiFile `json:"releases"`
}

type pypiFile struct {
	Yanked       bool    `json:"yanked"`
	YankedReason *string `json:"yanked_reason"`
}

// yanked reports whether every file of a release is yanked, which is what
// PEP 592 means by a yanked release, and the reason given for it.
func pypiYanked(files []pypiFile) (string, bool) {
	if len(files) == 0 {
		return "", false
	}
	reason := ""
	for _, f := range files {
		if !f.Yanked {
			return "", false
		}
		if f.YankedReason != nil && *f.YankedReason != "" {
			reason = *f.YankedReason
		}
	}
	return reason, true
}

func (r *PyPIResolver) fetch(ctx context.Context, pkg string) (pypiResponse, error) {
	body, err := r.docs.get(ctx, r.client, fmt.Sprintf("%s/pypi/%s/json", r.baseURL, pkg), "PyPI registry", "package "+pkg, nil)
	if err != nil {
		httpclient.LoggerFromContext(ctx).Warn("failed to fetch PyPI data", "package", pkg, "error", err)
		return pypiResponse{}, err
	}
	return decodeDoc[pypiResponse](body, "PyPI")
}

func (r *PyPIResolver) Resolve(ctx context.Context, pkg string, pattern *parser.VersionPattern) (string, error) {
	logger := httpclient.LoggerFromContext(ctx)
	logger.Debug("resolving package", "resolver", "pypi", "package", pkg)

	data, err := r.fetch(ctx, pkg)
	if err != nil {
		return "", err
	}

	versions := make([]string, 0, len(data.Releases))
	for v, files := range data.Releases {
		if _, yanked := pypiYanked(files); yanked {
			continue
		}
		versions = append(versions, v)
	}

	sort.Slice(versions, func(i, j int) bool {
		return version.Compare(version.Parse(versions[i]), version.Parse(versions[j])) < 0
	})

	for i := len(versions) - 1; i >= 0; i-- {
		if pattern.Matches(versions[i]) {
			logger.Info("resolved package", "resolver", "pypi", "package", pkg, "version", versions[i])
			return versions[i], nil
		}
	}

	return "", fmt.Errorf("no matching version found for package %s with pattern %v", pkg, pattern)
}

// Yanked reports whether the pinned release has been yanked since.
func (r *PyPIResolver) Yanked(ctx context.Context, pkg, v string) (string, bool, error) {
	data, err := r.fetch(ctx, pkg)
	if err != nil {
		return "", false, err
	}
	reason, yanked := pypiYanked(data.Releases[v])
	return reason, yanked, nil
}

func (r *PyPIResolver) GetChangelog(ctx context.Context, pkg string, from, to string) (string, error) {
	return "", nil
}
